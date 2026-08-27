package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/database"
	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/payment"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/utils"
)

// PaymentService 负责支付渠道管理、发起支付与处理回调。
//
// 它是 payment 包（协议层）与 OrderService（业务层）之间的唯一桥梁：
// adapter 只管验签解析，业务处理一律转交 OrderService.HandlePaymentSuccess。
type PaymentService struct {
	db       *database.DB
	repo     *repository.PaymentRepo
	orders   *OrderService
	settings *SettingService
	notifier *NotifyService
	baseURL  string // 后端外部地址，用于拼 notify_url
	frontURL string // 前端外部地址，用于拼 return_url
}

// NewPaymentService 构造。
func NewPaymentService(
	db *database.DB,
	repo *repository.PaymentRepo,
	orders *OrderService,
	settings *SettingService,
	notifier *NotifyService,
	baseURL, frontendURL string,
) *PaymentService {
	return &PaymentService{
		db:       db,
		repo:     repo,
		orders:   orders,
		settings: settings,
		notifier: notifier,
		baseURL:  strings.TrimRight(baseURL, "/"),
		frontURL: strings.TrimRight(frontendURL, "/"),
	}
}

// ---------------------------------------------------------------------------
// 渠道配置
// ---------------------------------------------------------------------------

// parseConfig 把渠道存储的 JSON 配置解析成 map。
func parseConfig(raw string) map[string]string {
	out := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		// 兼容历史上可能存在的 map[string]any 格式
		var anyMap map[string]any
		if err2 := json.Unmarshal([]byte(raw), &anyMap); err2 == nil {
			for k, v := range anyMap {
				out[k] = fmt.Sprint(v)
			}
		}
	}
	return out
}

func encodeConfig(cfg map[string]string) (string, error) {
	b, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// PublicChannel 是前台可见的支付方式（绝不含任何配置）。
type PublicChannel struct {
	ID       uint64 `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Icon     string `json:"icon"`
	Sort     int    `json:"sort"`
}

// ListPublicChannels 返回前台可用的支付方式。
func (s *PaymentService) ListPublicChannels(ctx context.Context) ([]PublicChannel, error) {
	list, err := s.repo.ListChannels(ctx, nil, true)
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	out := make([]PublicChannel, 0, len(list))
	for _, c := range list {
		// 只有代码里真的注册了该 provider 才展示，避免用户点了一个无法工作的方式
		if !payment.IsRegistered(c.Provider) {
			continue
		}
		out = append(out, PublicChannel{
			ID: c.ID, Name: c.Name, Provider: c.Provider, Icon: c.Icon, Sort: c.Sort,
		})
	}
	return out, nil
}

// AdminChannel 是后台可见的渠道（配置已脱敏）。
type AdminChannel struct {
	model.PaymentChannel
	Config    map[string]string `json:"config"`
	NotifyURL string            `json:"notify_url"`
	Available bool              `json:"available"` // provider 是否已在代码中注册
}

// maskChannelConfig 对渠道配置中的敏感项脱敏。
func maskChannelConfig(provider string, cfg map[string]string) map[string]string {
	secrets := payment.SecretFields(provider)
	out := make(map[string]string, len(cfg))
	for k, v := range cfg {
		if secrets[k] && v != "" {
			out[k] = utils.MaskSecret(v)
			continue
		}
		out[k] = v
	}
	return out
}

// ListAdminChannels 返回后台渠道列表。
func (s *PaymentService) ListAdminChannels(ctx context.Context) ([]AdminChannel, error) {
	list, err := s.repo.ListChannels(ctx, nil, false)
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	out := make([]AdminChannel, 0, len(list))
	for _, c := range list {
		out = append(out, AdminChannel{
			PaymentChannel: c,
			Config:         maskChannelConfig(c.Provider, parseConfig(c.Config)),
			NotifyURL:      s.NotifyURL(c.Provider, c.ID),
			Available:      payment.IsRegistered(c.Provider),
		})
	}
	return out, nil
}

// GetAdminChannel 返回单个渠道（脱敏）。
func (s *PaymentService) GetAdminChannel(ctx context.Context, id uint64) (*AdminChannel, error) {
	c, err := s.repo.FindChannel(ctx, nil, id)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, api.NewError(api.CodePaymentChannelNotFound)
		}
		return nil, api.WrapError(api.CodeInternal, err)
	}
	return &AdminChannel{
		PaymentChannel: *c,
		Config:         maskChannelConfig(c.Provider, parseConfig(c.Config)),
		NotifyURL:      s.NotifyURL(c.Provider, c.ID),
		Available:      payment.IsRegistered(c.Provider),
	}, nil
}

// ChannelInput 是创建/更新渠道的入参。
type ChannelInput struct {
	Name     string            `json:"name" binding:"required"`
	Provider string            `json:"provider" binding:"required"`
	Icon     string            `json:"icon"`
	Config   map[string]string `json:"config"`
	Status   string            `json:"status"`
	Sort     int               `json:"sort"`
	Remark   string            `json:"remark"`
}

// mergeSecrets 合并配置：敏感字段若提交的是掩码值，保留数据库中的旧值。
//
// 这是 §57 明确要求的行为。没有它，管理员只是改了个排序，
// 保存后支付密钥就会变成 "sk_l********"，所有支付立刻全挂。
func mergeSecrets(provider string, incoming, existing map[string]string) map[string]string {
	secrets := payment.SecretFields(provider)
	out := make(map[string]string, len(incoming))
	for k, v := range incoming {
		if secrets[k] && utils.IsSecretUnchanged(v) {
			if old, ok := existing[k]; ok {
				out[k] = old
				continue
			}
			// 旧值也没有，说明是新建时留空，跳过让必填校验去报错
			continue
		}
		out[k] = strings.TrimSpace(v)
	}
	// 保留本次未提交、但数据库里已有的敏感项
	for k, v := range existing {
		if _, submitted := out[k]; !submitted && secrets[k] {
			out[k] = v
		}
	}
	return out
}

// CreateChannel 新建支付渠道。
func (s *PaymentService) CreateChannel(ctx context.Context, in *ChannelInput) (*model.PaymentChannel, error) {
	if !payment.IsRegistered(in.Provider) {
		return nil, api.NewErrorf(api.CodePaymentNotSupported, "不支持的支付渠道类型: %s", in.Provider)
	}
	cfg := payment.ApplyDefaults(in.Provider, mergeSecrets(in.Provider, in.Config, nil))
	if err := payment.ValidateConfig(in.Provider, cfg); err != nil {
		return nil, api.NewErrorf(api.CodePaymentConfigInvalid, "%s", err.Error())
	}
	// 用真实配置构造一次，把"私钥格式错误"这类问题挡在保存之前
	if _, err := payment.Build(in.Provider, cfg); err != nil {
		return nil, api.NewErrorf(api.CodePaymentConfigInvalid, "%s", err.Error())
	}

	raw, err := encodeConfig(cfg)
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	c := &model.PaymentChannel{
		Name:     utils.TrimAndLimit(in.Name, 60),
		Provider: in.Provider,
		Icon:     utils.TrimAndLimit(in.Icon, 250),
		Config:   raw,
		Status:   normalizeChannelStatus(in.Status),
		Sort:     in.Sort,
		Remark:   utils.TrimAndLimit(in.Remark, 480),
	}
	if err := s.repo.CreateChannel(ctx, nil, c); err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	return c, nil
}

// UpdateChannel 更新支付渠道。
func (s *PaymentService) UpdateChannel(ctx context.Context, id uint64, in *ChannelInput) (*model.PaymentChannel, error) {
	existing, err := s.repo.FindChannel(ctx, nil, id)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, api.NewError(api.CodePaymentChannelNotFound)
		}
		return nil, api.WrapError(api.CodeInternal, err)
	}
	provider := existing.Provider
	if in.Provider != "" && in.Provider != provider {
		// 允许换 provider，但配置必须整体重填
		if !payment.IsRegistered(in.Provider) {
			return nil, api.NewErrorf(api.CodePaymentNotSupported, "不支持的支付渠道类型: %s", in.Provider)
		}
		provider = in.Provider
		existing.Config = "{}"
	}

	cfg := payment.ApplyDefaults(provider, mergeSecrets(provider, in.Config, parseConfig(existing.Config)))
	if err := payment.ValidateConfig(provider, cfg); err != nil {
		return nil, api.NewErrorf(api.CodePaymentConfigInvalid, "%s", err.Error())
	}
	if _, err := payment.Build(provider, cfg); err != nil {
		return nil, api.NewErrorf(api.CodePaymentConfigInvalid, "%s", err.Error())
	}

	raw, err := encodeConfig(cfg)
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	fields := map[string]any{
		"name":     utils.TrimAndLimit(in.Name, 60),
		"provider": provider,
		"icon":     utils.TrimAndLimit(in.Icon, 250),
		"config":   raw,
		"status":   normalizeChannelStatus(in.Status),
		"sort":     in.Sort,
		"remark":   utils.TrimAndLimit(in.Remark, 480),
	}
	if err := s.repo.UpdateChannel(ctx, nil, id, fields); err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	return s.repo.FindChannel(ctx, nil, id)
}

// DeleteChannel 删除渠道。已产生订单的渠道只允许禁用。
func (s *PaymentService) DeleteChannel(ctx context.Context, id uint64) error {
	has, err := s.repo.ChannelHasOrders(ctx, nil, id)
	if err != nil {
		return api.WrapError(api.CodeInternal, err)
	}
	if has {
		return api.NewErrorf(api.CodeConflict,
			"该支付渠道已产生历史订单，只能禁用不能删除（删除会导致订单支付信息无从追溯）")
	}
	return wrapServiceErr(s.repo.DeleteChannel(ctx, nil, id))
}

func normalizeChannelStatus(s string) string {
	if s == model.ChannelEnabled {
		return model.ChannelEnabled
	}
	return model.ChannelDisabled
}

// TestChannel 测试渠道配置是否可用。
//
// 通过发起一笔极小额的测试支付来验证 —— 只创建支付单，不真的付款。
// 能拿到支付地址就说明网关地址、商户号、密钥、签名算法都是对的。
func (s *PaymentService) TestChannel(ctx context.Context, id uint64) (map[string]any, error) {
	provider, channel, err := s.buildProvider(ctx, id)
	if err != nil {
		return nil, err
	}

	testOrderNo := "TEST" + utils.GenerateOrderNo()
	resp, err := provider.CreatePayment(ctx, payment.PaymentRequest{
		OrderNo:   testOrderNo,
		Subject:   "MoeCard 配置测试",
		Amount:    1, // 1 分
		Currency:  s.settings.Get(model.SetCurrency),
		NotifyURL: s.NotifyURL(channel.Provider, channel.ID),
		ReturnURL: s.frontURL + "/pay/result",
		Device:    "pc",
	})
	if err != nil {
		return nil, api.NewErrorf(api.CodePaymentConfigInvalid, "测试失败: %s", err.Error())
	}

	out := map[string]any{
		"ok":         true,
		"action":     resp.Action,
		"notify_url": s.NotifyURL(channel.Provider, channel.ID),
		"message":    "配置有效，已成功创建测试支付单（未产生真实扣款）",
	}
	if resp.URL != "" {
		out["url"] = resp.URL
	}
	if resp.QRCode != "" {
		out["qrcode"] = resp.QRCode
	}
	if resp.Action == payment.ActionForm {
		out["message"] = "配置有效，已生成支付跳转表单（未产生真实扣款）"
	}
	return out, nil
}

// buildProvider 按渠道 ID 构造 Provider 实例。
func (s *PaymentService) buildProvider(ctx context.Context, channelID uint64) (payment.Provider, *model.PaymentChannel, error) {
	channel, err := s.repo.FindChannel(ctx, nil, channelID)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, nil, api.NewError(api.CodePaymentChannelNotFound)
		}
		return nil, nil, api.WrapError(api.CodeInternal, err)
	}
	p, err := payment.Build(channel.Provider, parseConfig(channel.Config))
	if err != nil {
		return nil, nil, api.NewErrorf(api.CodePaymentConfigInvalid,
			"支付渠道「%s」配置有误: %s", channel.Name, err.Error())
	}
	return p, channel, nil
}

// NotifyURL 生成某渠道的异步回调地址。
//
// URL 中同时带 provider 与 channel_id：
//   - channel_id 用于定位正确的密钥去验签
//   - provider 用于交叉校验，防止攻击者拿"弱签名渠道"的 URL
//     去冒充"强签名渠道"的通知
func (s *PaymentService) NotifyURL(provider string, channelID uint64) string {
	return fmt.Sprintf("%s/api/v1/payments/notify/%s/%d", s.baseURL, provider, channelID)
}

// ReturnURL 生成同步跳转地址。
func (s *PaymentService) ReturnURL(orderNo string) string {
	return fmt.Sprintf("%s/pay/result?order_no=%s", s.frontURL, orderNo)
}

// ---------------------------------------------------------------------------
// 发起支付
// ---------------------------------------------------------------------------

// PayResult 是发起支付的结果。
type PayResult struct {
	Action   string `json:"action"` // redirect | qrcode | form
	URL      string `json:"url,omitempty"`
	QRCode   string `json:"qrcode,omitempty"`
	FormHTML string `json:"form_html,omitempty"`
	OrderNo  string `json:"order_no"`
}

// CreatePayment 为订单发起支付。
func (s *PaymentService) CreatePayment(ctx context.Context, orderNo string, channelID uint64, device, clientIP string) (*PayResult, error) {
	order, items, err := s.orders.GetByNo(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if order.IsPaidLike() {
		return nil, api.NewError(api.CodeOrderAlreadyPaid)
	}
	if !order.IsPayable() {
		return nil, api.NewErrorf(api.CodeOrderStatusInvld,
			"订单当前状态（%s）不能支付", model.OrderStatusLabel(order.Status))
	}
	if order.IsExpiredAt(utils.NowUTC()) {
		return nil, api.NewError(api.CodeOrderExpired)
	}

	provider, channel, err := s.buildProvider(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if !channel.IsEnabled() {
		return nil, api.NewError(api.CodePaymentChannelNotFound)
	}

	subject := "订单 " + order.OrderNo
	if len(items) > 0 {
		subject = items[0].ProductName
		if len(items) > 1 {
			subject = fmt.Sprintf("%s 等 %d 件商品", subject, len(items))
		}
	}

	req := payment.PaymentRequest{
		OrderNo:   order.OrderNo,
		Subject:   subject,
		Amount:    order.PayAmount,
		Currency:  s.settings.Get(model.SetCurrency),
		NotifyURL: s.NotifyURL(channel.Provider, channel.ID),
		ReturnURL: s.ReturnURL(order.OrderNo),
		ClientIP:  clientIP,
		Email:     order.Email,
		Device:    device,
		Extra:     map[string]any{},
	}
	if order.ExpiredAt != nil {
		req.Extra["expire_at"] = *order.ExpiredAt
	}

	resp, err := provider.CreatePayment(ctx, req)
	if err != nil {
		s.writeLog(ctx, order, channel, model.PayEventCreateFailed, "", err.Error(), clientIP)
		logger.Payment().Error("发起支付失败",
			"order_no", order.OrderNo, "provider", channel.Provider, "err", err)
		return nil, api.NewErrorf(api.CodePaymentFailed, "发起支付失败: %s", err.Error())
	}

	if err := s.orders.MarkPaying(ctx, order.ID, channel.ID, channel.Name, channel.Provider); err != nil {
		return nil, err
	}
	s.writeLog(ctx, order, channel, model.PayEventCreate, resp.TradeNo, resp.Raw, clientIP)

	return &PayResult{
		Action:   resp.Action,
		URL:      resp.URL,
		QRCode:   resp.QRCode,
		FormHTML: resp.FormHTML,
		OrderNo:  order.OrderNo,
	}, nil
}

// ---------------------------------------------------------------------------
// 处理回调
// ---------------------------------------------------------------------------

// NotifyOutcome 是回调处理结果，handler 据此写 HTTP 响应。
type NotifyOutcome struct {
	Body        string
	ContentType string
	StatusCode  int
}

// HandleNotify 处理支付平台的异步通知。
//
// 安全链条（缺一不可）：
//  1. 渠道存在且 URL 中的 provider 与渠道记录一致
//  2. provider.VerifyNotify 验签通过 ——**唯一**的支付真实性判定点
//  3. 平台声明支付成功
//  4. 转交 OrderService.HandlePaymentSuccess 做金额校验、幂等与发货
//
// 无论成功失败都写 payment_logs，便于事后排查与对账。
func (s *PaymentService) HandleNotify(ctx context.Context, providerKey string, channelID uint64, req payment.NotifyRequest) *NotifyOutcome {
	fallbackFail := &NotifyOutcome{Body: "fail", ContentType: "text/plain; charset=utf-8", StatusCode: 400}

	channel, err := s.repo.FindChannel(ctx, nil, channelID)
	if err != nil {
		logger.Payment().Error("回调指向的支付渠道不存在", "channel_id", channelID, "provider", providerKey)
		return fallbackFail
	}
	// 交叉校验：防止用 A 渠道（弱签名）的回调地址去冒充 B 渠道
	if channel.Provider != providerKey {
		logger.Payment().Error("回调 URL 中的 provider 与渠道记录不一致",
			"channel_id", channelID, "url_provider", providerKey, "channel_provider", channel.Provider)
		return fallbackFail
	}

	provider, err := payment.Build(channel.Provider, parseConfig(channel.Config))
	if err != nil {
		logger.Payment().Error("构造支付渠道失败", "channel_id", channelID, "err", err)
		return fallbackFail
	}

	req.ChannelID = channelID
	result, verifyErr := provider.VerifyNotify(ctx, req)

	// 验签失败：记录原始报文（已脱敏）用于排查，绝不触碰任何业务数据
	if verifyErr != nil || result == nil || !result.Success {
		event := model.PayEventNotifyInvalid
		status := "验签失败"
		if verifyErr == nil && result != nil {
			event = model.PayEventNotify
			status = "平台声明未支付成功: " + result.Status
		} else if verifyErr != nil {
			status = verifyErr.Error()
		}

		orderNo := ""
		if result != nil {
			orderNo = result.OrderNo
		}
		s.writeRawLog(ctx, orderNo, channel, event, "", status, sanitizeNotifyBody(req), req.RemoteIP)

		if verifyErr != nil {
			logger.Payment().Error("支付回调验签失败",
				"channel_id", channelID, "provider", channel.Provider,
				"remote_ip", req.RemoteIP, "err", verifyErr)
		}

		if result != nil && result.ResponseBody != "" {
			return &NotifyOutcome{
				Body:        result.ResponseBody,
				ContentType: result.ResponseContentType,
				StatusCode:  orDefaultInt(result.ResponseStatus, 200),
			}
		}
		return fallbackFail
	}

	// 验签通过且平台声明支付成功 → 交给业务层统一处理
	rawJSON, _ := json.Marshal(result.Raw)
	_, bizErr := s.orders.HandlePaymentSuccess(ctx, PaymentSuccessInput{
		OrderNo:   result.OrderNo,
		TradeNo:   result.TradeNo,
		Amount:    result.Amount,
		Provider:  channel.Provider,
		ChannelID: channel.ID,
		ClientIP:  req.RemoteIP,
		RawNotify: string(rawJSON),
	})
	if bizErr != nil {
		logger.Payment().Error("支付成功但业务处理失败",
			"order_no", result.OrderNo, "provider", channel.Provider, "err", bizErr)

		// 永久性错误 → 返回 4xx，明确告诉平台"别再重试了"。
		// 这类错误再试一万次也不会变：订单不存在就是不存在，金额不符就是被篡改。
		// 之前一律返回 500 会让支付平台按重试策略反复打过来，
		// 既刷爆日志，也可能被拿来当放大器。
		//
		// 其余（数据库抖动、发货流程报错等）才返回 500 让平台重试 ——
		// 我们的幂等设计保证重试是安全的。
		e := api.AsError(bizErr)
		switch e.Code {
		case api.CodePaymentAmountMismatch,
			api.CodePaymentChannelMismatch,
			api.CodeOrderNotFound,
			api.CodeOrderStatusInvld:
			return fallbackFail
		}
		return &NotifyOutcome{Body: "fail", ContentType: "text/plain; charset=utf-8", StatusCode: 500}
	}

	return &NotifyOutcome{
		Body:        result.ResponseBody,
		ContentType: orDefaultStr(result.ResponseContentType, "text/plain; charset=utf-8"),
		StatusCode:  orDefaultInt(result.ResponseStatus, 200),
	}
}

// SyncOrderStatus 主动向支付平台查询并同步订单状态。
//
// 用途：回调丢失时的兜底。前端支付结果页轮询时会触发。
// 同样走 HandlePaymentSuccess，因此和回调路径共享全部幂等与校验逻辑。
func (s *PaymentService) SyncOrderStatus(ctx context.Context, orderNo string) (*model.Order, error) {
	order, _, err := s.orders.GetByNo(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	// 已支付或未选择支付方式时无需查询
	if order.IsPaidLike() || order.PaymentChannelID == 0 {
		return order, nil
	}

	provider, channel, err := s.buildProvider(ctx, order.PaymentChannelID)
	if err != nil {
		logger.Payment().Warn("主动查询时构造渠道失败", "order_no", orderNo, "err", err)
		return order, nil // 查询失败不影响前端展示当前状态
	}

	status, err := provider.QueryPayment(ctx, payment.QueryRequest{
		OrderNo: orderNo,
		TradeNo: order.PaymentTradeNo,
	})
	if err != nil {
		logger.Payment().Warn("主动查询支付状态失败",
			"order_no", orderNo, "provider", channel.Provider, "err", err)
		return order, nil
	}
	s.writeLog(ctx, order, channel, model.PayEventQuery, status.TradeNo, status.Raw, "")

	if !status.Paid {
		return order, nil
	}

	logger.Payment().Info("主动查询发现订单已支付，补做业务处理",
		"order_no", orderNo, "trade_no", status.TradeNo)

	res, err := s.orders.HandlePaymentSuccess(ctx, PaymentSuccessInput{
		OrderNo:   orderNo,
		TradeNo:   status.TradeNo,
		Amount:    status.Amount,
		Provider:  channel.Provider,
		ChannelID: channel.ID,
		RawNotify: status.Raw,
	})
	if err != nil {
		return nil, err
	}
	return res.Order, nil
}

// Refund 发起退款。
//
// 若渠道不支持自动退款（如易支付 V1），返回明确错误，
// 由后台降级为「人工退款」—— 但订单侧的退款记录依然完整。
func (s *PaymentService) Refund(ctx context.Context, orderID uint64, in RefundInput) (*model.Order, error) {
	order, _, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	amount := in.Amount
	if amount <= 0 {
		amount = order.PayAmount
	}

	if in.Manual {
		// 人工退款：只记账，不调渠道接口
		o, err := s.orders.MarkRefunded(ctx, orderID, amount, in.Reason)
		if err != nil {
			return nil, err
		}
		s.writeRawLog(ctx, order.OrderNo, nil, model.PayEventManualRefund, order.PaymentTradeNo,
			fmt.Sprintf("管理员标记人工退款 %s", utils.FormatAmount(amount)), in.Reason, "")
		s.notifier.Refunded(o, amount, true)
		return o, nil
	}

	if order.PaymentChannelID == 0 {
		return nil, api.NewErrorf(api.CodeRefundNotSupported, "该订单没有关联支付渠道，请使用人工退款")
	}
	provider, channel, err := s.buildProvider(ctx, order.PaymentChannelID)
	if err != nil {
		return nil, err
	}

	refundNo := "RF" + utils.GenerateOrderNo()
	res, err := provider.Refund(ctx, payment.RefundRequest{
		OrderNo:     order.OrderNo,
		TradeNo:     order.PaymentTradeNo,
		RefundNo:    refundNo,
		TotalAmount: order.PayAmount,
		Amount:      amount,
		Currency:    s.settings.Get(model.SetCurrency),
		Reason:      in.Reason,
	})
	if err != nil {
		s.writeLog(ctx, order, channel, model.PayEventRefundFailed, order.PaymentTradeNo, err.Error(), "")
		if strings.Contains(err.Error(), payment.ErrNotSupported.Error()) {
			return nil, api.NewErrorf(api.CodeRefundNotSupported,
				"支付渠道「%s」不支持自动退款，请在支付平台后台操作后使用「人工退款」记账", channel.Name)
		}
		return nil, api.NewErrorf(api.CodeRefundFailed, "退款失败: %s", err.Error())
	}

	o, err := s.orders.MarkRefunded(ctx, orderID, amount, in.Reason)
	if err != nil {
		// 钱已经退出去了但记账失败 —— 必须高优先级告警
		logger.Payment().Error("退款成功但订单状态更新失败，需人工核对",
			"order_no", order.OrderNo, "refund_no", refundNo, "err", err)
		return nil, err
	}
	s.writeLog(ctx, order, channel, model.PayEventRefund, res.RefundNo, res.Raw, "")
	s.notifier.Refunded(o, amount, false)
	return o, nil
}

// ---------------------------------------------------------------------------
// 日志
// ---------------------------------------------------------------------------

func (s *PaymentService) writeLog(ctx context.Context, order *model.Order, channel *model.PaymentChannel, event, tradeNo, raw, ip string) {
	orderNo := ""
	var orderID uint64
	var amount int64
	if order != nil {
		orderNo, orderID, amount = order.OrderNo, order.ID, order.PayAmount
	}
	l := &model.PaymentLog{
		OrderID:      orderID,
		OrderNo:      orderNo,
		Event:        event,
		Amount:       amount,
		TradeNo:      tradeNo,
		ResponseData: raw,
		ClientIP:     ip,
	}
	if channel != nil {
		l.ChannelID, l.Provider = channel.ID, channel.Provider
	}
	if err := s.repo.CreateLog(ctx, nil, l); err != nil {
		logger.Payment().Error("写入支付日志失败", "order_no", orderNo, "err", err)
	}
}

func (s *PaymentService) writeRawLog(ctx context.Context, orderNo string, channel *model.PaymentChannel, event, tradeNo, status, raw, ip string) {
	l := &model.PaymentLog{
		OrderNo:     orderNo,
		Event:       event,
		TradeNo:     tradeNo,
		Status:      utils.TrimAndLimit(status, 250),
		RequestData: raw,
		ClientIP:    ip,
	}
	if channel != nil {
		l.ChannelID, l.Provider = channel.ID, channel.Provider
	}
	if err := s.repo.CreateLog(ctx, nil, l); err != nil {
		logger.Payment().Error("写入支付日志失败", "order_no", orderNo, "err", err)
	}
}

// sanitizeNotifyBody 把回调原始内容脱敏后用于落库。
//
// 支付回调本身通常不含密钥，但 sign 字段与部分平台的扩展参数可能敏感；
// 统一过滤后再落库，避免日志泄露时被用于分析。
func sanitizeNotifyBody(req payment.NotifyRequest) string {
	parts := map[string]any{}
	if len(req.Query) > 0 {
		parts["query"] = utils.SanitizeMap(payment.ValuesToMap(req.Query))
	}
	if len(req.Form) > 0 {
		parts["form"] = utils.SanitizeMap(payment.ValuesToMap(req.Form))
	}
	if len(req.Body) > 0 && len(req.Form) == 0 {
		body := string(req.Body)
		var m map[string]any
		if err := json.Unmarshal(req.Body, &m); err == nil {
			parts["body"] = utils.SanitizeAnyMap(m)
		} else {
			parts["body"] = utils.TrimAndLimit(body, 4000)
		}
	}
	b, err := json.Marshal(parts)
	if err != nil {
		return ""
	}
	return utils.TrimAndLimit(string(b), 8000)
}

// ListLogs 查询支付日志。
func (s *PaymentService) ListLogs(ctx context.Context, q repository.PaymentLogQuery) ([]model.PaymentLog, int64, error) {
	list, total, err := s.repo.ListLogs(ctx, nil, q)
	if err != nil {
		return nil, 0, api.WrapError(api.CodeInternal, err)
	}
	return list, total, nil
}

// Descriptors 返回全部已注册 provider 的配置描述（驱动后台表单）。
func (s *PaymentService) Descriptors() []payment.Descriptor { return payment.Descriptors() }

func orDefaultStr(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func orDefaultInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

var _ = gorm.ErrRecordNotFound
