package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/notify"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/utils"
)

// NotifyService 负责把需要商家立刻知道的事推送出去。
//
// 三条铁律，和邮件服务一致：
//  1. 绝不阻塞业务事务 —— 一律异步，用独立 context
//  2. 绝不因为通知失败而影响订单/发货
//  3. 每次投递都留痕，否则通知悄悄失效了没人会发现
type NotifyService struct {
	settings *SettingService
	repo     *repository.NotifyRepo
	mailer   *MailService
	baseURL  string
}

// NewNotifyService 构造。
func NewNotifyService(settings *SettingService, repo *repository.NotifyRepo, mailer *MailService, baseURL string) *NotifyService {
	// 把各渠道的密钥字段登记为敏感项，出参才会脱敏、
	// 提交脱敏值时才会保留旧值（与支付渠道同样的语义）。
	for _, d := range notify.Descriptors() {
		for _, f := range d.Fields {
			key := model.NotifyConfigKey(d.Key, f.Key)
			if f.Secret {
				model.RegisterSecretSettingKey(key)
			} else {
				model.RegisterKnownSettingKey(key)
			}
		}
	}
	return &NotifyService{
		settings: settings, repo: repo, mailer: mailer,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

// Enabled 判断通知总开关是否打开且至少配置了一个渠道。
func (s *NotifyService) Enabled() bool {
	return s.settings.GetBool(model.SetNotifyEnabled) && len(s.activeChannels()) > 0
}

// activeChannels 返回已启用的渠道 key 列表。
func (s *NotifyService) activeChannels() []string {
	raw := s.settings.Get(model.SetNotifyChannels)
	var out []string
	for _, k := range strings.Split(raw, ",") {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// channelConfig 取出某渠道的配置（含解密后的敏感字段）。
func (s *NotifyService) channelConfig(channel string) map[string]string {
	cfg := map[string]string{}
	for _, d := range notify.Descriptors() {
		if d.Key != channel {
			continue
		}
		for _, f := range d.Fields {
			cfg[f.Key] = s.settings.Get(model.NotifyConfigKey(channel, f.Key))
		}
	}
	return cfg
}

// buildProvider 构造渠道实例，并把外部依赖显式注入进去。
func (s *NotifyService) buildProvider(channel string, cfg map[string]string) (notify.Provider, error) {
	return notify.Build(channel, cfg, notify.Deps{Mail: s.mailer})
}

// eventEnabled 判断某类事件是否需要推送。
func (s *NotifyService) eventEnabled(event string) bool {
	switch event {
	case notify.EventOrderPaid:
		return s.settings.GetBool(model.SetNotifyOnPaid)
	case notify.EventManualDelivery:
		return s.settings.GetBool(model.SetNotifyOnManual)
	case notify.EventNeedsAttention:
		return s.settings.GetBool(model.SetNotifyOnAttention)
	case notify.EventLowStock:
		return s.settings.GetBool(model.SetNotifyOnLowStock)
	case notify.EventRefund:
		return s.settings.GetBool(model.SetNotifyOnRefund)
	case notify.EventTest:
		return true // 后台手动点的测试，不受事件开关约束
	default:
		return false
	}
}

// Dispatch 异步投递一条通知。调用方无需处理错误 —— 失败只记日志。
func (s *NotifyService) Dispatch(msg *notify.Message) {
	if msg == nil || !s.Enabled() || !s.eventEnabled(msg.Event) {
		return
	}
	m := *msg // 拷贝，避免调用方后续修改
	go func() {
		// 独立 context：调用方的 HTTP 请求可能早已结束
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.sendAll(ctx, &m)
	}()
}

// SendTest 同步向指定渠道发送测试消息（后台按钮需要拿到结果）。
func (s *NotifyService) SendTest(ctx context.Context, channel string, cfg map[string]string) error {
	// cfg 为空表示用已保存的配置
	if len(cfg) == 0 {
		cfg = s.channelConfig(channel)
	} else {
		// 敏感字段提交的是脱敏值时，回退到已保存的真实值
		saved := s.channelConfig(channel)
		for k, v := range cfg {
			if utils.IsSecretUnchanged(v) {
				cfg[k] = saved[k]
			}
		}
	}

	p, err := s.buildProvider(channel, cfg)
	if err != nil {
		return err
	}
	msg := &notify.Message{
		Event:    notify.EventTest,
		Title:    "【" + s.settings.SiteName() + "】通知测试",
		Body:     "如果你看到这条消息，说明通知渠道已经配置成功。",
		Priority: notify.PriorityNormal,
		Fields: []notify.Field{
			{Key: "渠道", Value: channel},
			{Key: "时间", Value: utils.FormatInZone(utils.NowUTC(), s.settings.Timezone(), "2006-01-02 15:04:05")},
		},
		URL: s.adminURL("/admin/settings"),
	}
	err = p.Send(ctx, msg)
	s.log(ctx, channel, msg, err)
	return err
}

// sendAll 向所有已启用渠道投递。一个渠道失败不影响其他渠道。
func (s *NotifyService) sendAll(ctx context.Context, msg *notify.Message) {
	for _, ch := range s.activeChannels() {
		p, err := s.buildProvider(ch, s.channelConfig(ch))
		if err != nil {
			logger.L().Warn("通知渠道配置无效", "channel", ch, "err", err)
			s.log(ctx, ch, msg, err)
			continue
		}
		if err := p.Send(ctx, msg); err != nil {
			logger.L().Warn("通知发送失败", "channel", ch, "event", msg.Event, "err", err)
		}
		s.log(ctx, ch, msg, err)
	}
}

// log 写入 notify_logs。写日志本身失败也只是打一条 error，不再向上传播。
func (s *NotifyService) log(ctx context.Context, channel string, msg *notify.Message, sendErr error) {
	row := &model.NotifyLog{
		Channel:   channel,
		Event:     msg.Event,
		Title:     utils.TrimAndLimit(msg.Title, 250),
		Content:   utils.TrimAndLimit(msg.Text(), 2000),
		Status:    model.NotifyStatusSuccess,
		CreatedAt: utils.NowUTC(),
	}
	if sendErr != nil {
		row.Status = model.NotifyStatusFailed
		row.Error = utils.TrimAndLimit(sendErr.Error(), 900)
	}
	if err := s.repo.Create(ctx, nil, row); err != nil {
		logger.L().Error("写入通知日志失败", "err", err)
	}
}

// adminURL 拼出后台页面的完整地址，方便从通知里一键跳转。
func (s *NotifyService) adminURL(path string) string {
	if s.baseURL == "" {
		return ""
	}
	return s.baseURL + path
}

// ---- 业务事件封装 ----

// OrderPaid 订单支付成功。
//
// 手动发货的订单单独发一条更醒目的通知：那意味着有人正在等你操作。
func (s *NotifyService) OrderPaid(order *model.Order, items []model.OrderItem) {
	if order == nil {
		return
	}
	name, qty := firstItemBrief(items)

	event := notify.EventOrderPaid
	title := "💰 新订单已支付"
	priority := notify.PriorityNormal
	if order.DeliveryType == model.DeliveryManual {
		event = notify.EventManualDelivery
		title = "📦 有订单等待人工发货"
		priority = notify.PriorityUrgent
	}

	s.Dispatch(&notify.Message{
		Event:    event,
		Title:    title,
		Priority: priority,
		Fields: []notify.Field{
			{Key: "商品", Value: name},
			{Key: "数量", Value: fmt.Sprint(qty)},
			{Key: "金额", Value: s.settings.CurrencySymbol() + utils.FormatAmount(order.PayAmount)},
			{Key: "买家", Value: utils.MaskEmail(order.Email)},
			{Key: "订单号", Value: order.OrderNo},
		},
		URL: s.adminURL("/admin/orders?order_no=" + order.OrderNo),
	})
}

// NeedsAttention 订单收了钱但发不出货 —— 最高优先级。
func (s *NotifyService) NeedsAttention(order *model.Order, reason string) {
	if order == nil {
		return
	}
	s.Dispatch(&notify.Message{
		Event:    notify.EventNeedsAttention,
		Title:    "⚠️ 订单需要人工处理",
		Body:     "买家已付款但系统无法自动发货，请尽快处理，否则会引发投诉。",
		Priority: notify.PriorityUrgent,
		Fields: []notify.Field{
			{Key: "原因", Value: reason},
			{Key: "金额", Value: s.settings.CurrencySymbol() + utils.FormatAmount(order.PayAmount)},
			{Key: "买家", Value: utils.MaskEmail(order.Email)},
			{Key: "订单号", Value: order.OrderNo},
		},
		URL: s.adminURL("/admin/orders?order_no=" + order.OrderNo),
	})
}

// LowStock 卡密库存告急。
func (s *NotifyService) LowStock(product *model.Product, remain int64, threshold int) {
	if product == nil {
		return
	}
	body := "库存不足会让商品直接显示售罄，订单会白白流失。"
	if remain == 0 {
		body = "库存已经耗尽，商品当前无法下单。"
	}
	s.Dispatch(&notify.Message{
		Event:    notify.EventLowStock,
		Title:    "📉 卡密库存告急",
		Body:     body,
		Priority: notify.PriorityUrgent,
		Fields: []notify.Field{
			{Key: "商品", Value: product.Name},
			{Key: "剩余", Value: fmt.Sprint(remain)},
			{Key: "告警阈值", Value: fmt.Sprint(threshold)},
		},
		URL: s.adminURL(fmt.Sprintf("/admin/products/%d/codes", product.ID)),
	})
}

// Refunded 发生退款。
func (s *NotifyService) Refunded(order *model.Order, amount int64, manual bool) {
	if order == nil {
		return
	}
	kind := "渠道退款"
	if manual {
		kind = "人工退款记账"
	}
	s.Dispatch(&notify.Message{
		Event:    notify.EventRefund,
		Title:    "↩️ 订单已退款",
		Priority: notify.PriorityNormal,
		Fields: []notify.Field{
			{Key: "方式", Value: kind},
			{Key: "退款金额", Value: s.settings.CurrencySymbol() + utils.FormatAmount(amount)},
			{Key: "订单号", Value: order.OrderNo},
		},
		URL: s.adminURL("/admin/orders?order_no=" + order.OrderNo),
	})
}

func firstItemBrief(items []model.OrderItem) (string, int) {
	if len(items) == 0 {
		return "(无明细)", 0
	}
	return items[0].ProductName, items[0].Quantity
}

// ListLogs 分页查询通知日志。
func (s *NotifyService) ListLogs(ctx context.Context, q repository.NotifyLogQuery) ([]model.NotifyLog, int64, error) {
	list, total, err := s.repo.List(ctx, nil, q)
	if err != nil {
		return nil, 0, api.WrapError(api.CodeInternal, err)
	}
	return list, total, nil
}
