package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/database"
	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/utils"
)

// OrderService 是订单业务的唯一入口。
//
// **架构红线**：所有支付渠道验签成功后，统一调用 HandlePaymentSuccess。
// 任何 payment adapter 里都不允许出现发货、扣库存、核销优惠券的逻辑。
type OrderService struct {
	db       *database.DB
	orders   *repository.OrderRepo
	products *repository.ProductRepo
	codes    *repository.CodeRepo
	payments *repository.PaymentRepo
	coupons  *CouponService
	settings *SettingService
	mailer   *MailService
	notifier *NotifyService
}

// NewOrderService 构造。
func NewOrderService(
	db *database.DB,
	repos *repository.Repositories,
	coupons *CouponService,
	settings *SettingService,
	mailer *MailService,
	notifier *NotifyService,
) *OrderService {
	return &OrderService{
		db:       db,
		orders:   repos.Order,
		products: repos.Product,
		codes:    repos.Code,
		payments: repos.Payment,
		coupons:  coupons,
		settings: settings,
		mailer:   mailer,
		notifier: notifier,
	}
}

// CreateOrderInput 是创建订单的入参。
type CreateOrderInput struct {
	ProductID  uint64 `json:"product_id" binding:"required"`
	Quantity   int    `json:"quantity" binding:"required,min=1"`
	Email      string `json:"email" binding:"required"`
	CouponCode string `json:"coupon_code"`
	// CustomData 是买家针对商品自定义字段填写的内容（字段 key -> 值）。
	CustomData map[string]string `json:"custom_data"`
	ClientIP   string            `json:"-"`
}

// CreateOrderResult 是创建订单的结果。
type CreateOrderResult struct {
	Order *model.Order      `json:"order"`
	Items []model.OrderItem `json:"items"`
	Token string            `json:"query_token"`
}

// CreateOrder 创建订单并**预占库存**。
//
// 为什么下单就预占而不是支付时才扣：
// 支付时才扣会出现"用户付完钱发现没货"的资损场景。
// 预占的代价是未支付订单会占用库存，用 order_expire_minutes + 定时释放解决。
//
// 事务内的完整步骤：
//  1. 校验商城开关、商品状态、数量范围
//  2. 计算金额、校验优惠券（**不扣减次数**）
//  3. 预占库存：auto → 锁定卡密；manual → CAS 扣减 stock
//  4. 写入订单与明细（含商品快照）
func (s *OrderService) CreateOrder(ctx context.Context, in CreateOrderInput) (*CreateOrderResult, error) {
	if !s.settings.AllowOrder() {
		return nil, api.NewError(api.CodeShopClosed)
	}
	if s.settings.MaintenanceMode() {
		return nil, api.NewError(api.CodeMaintenance)
	}

	email := utils.NormalizeEmail(in.Email)
	if err := utils.ValidateEmail(email); err != nil {
		return nil, api.NewErrorf(api.CodeValidation, "%s", err.Error())
	}
	if in.Quantity <= 0 {
		return nil, api.NewError(api.CodeOrderQtyInvalid)
	}

	var result CreateOrderResult
	err := s.db.Tx(ctx, func(tx *gorm.DB) error {
		// ---- 1. 商品校验 ----
		product, err := s.products.FindForUpdate(ctx, tx, in.ProductID)
		if err != nil {
			if database.IsNotFound(err) {
				return api.NewError(api.CodeProductNotFound)
			}
			return err
		}
		if !product.IsOnSale() {
			return api.NewError(api.CodeProductOffShelf)
		}
		minQ, maxQ := product.MinQuantity, product.MaxQuantity
		if minQ < 1 {
			minQ = 1
		}
		if maxQ < minQ {
			maxQ = 100
		}
		if in.Quantity < minQ || in.Quantity > maxQ {
			return api.NewErrorf(api.CodeOrderQtyInvalid, "购买数量需在 %d - %d 之间", minQ, maxQ)
		}

		// ---- 2. 金额与优惠券 ----
		originalAmount := product.Price * int64(in.Quantity)
		if originalAmount <= 0 {
			return api.NewErrorf(api.CodeValidation, "订单金额不合法")
		}

		discount := int64(0)
		var couponID uint64
		var couponCode string
		if strings.TrimSpace(in.CouponCode) != "" {
			d, err := s.coupons.Validate(ctx, tx, in.CouponCode, product.ID, originalAmount, email)
			if err != nil {
				return err
			}
			discount = d.DiscountAmount
			couponID = d.CouponID
			couponCode = d.CouponCode
		}
		payAmount := originalAmount - discount
		if payAmount < 0 {
			payAmount = 0
		}
		// 应付金额为 0 的订单没有任何支付渠道能受理（网关一律拒绝 0 元交易），
		// 但库存已经被预占 —— 结果就是一张付不掉、又占着卡密直到超时的僵尸单。
		// 与其让用户卡在支付页，不如在这里就把配置问题暴露出来。
		if payAmount <= 0 {
			return api.NewErrorf(api.CodeCouponNotApplicable,
				"优惠后应付金额为 0，无法发起支付。请更换优惠券或联系商家")
		}

		// ---- 2.5 买家自定义信息 ----
		// 代充这类手动发货商品必须拿到买家账号，否则商家收了钱也不知道充给谁。
		// 校验放在事务内、库存预占之前：校验不过就整体回滚，不会留下占着库存的废单。
		customData, err := ValidateCustomData(
			ParseCustomFields(product.CustomFields), in.CustomData)
		if err != nil {
			return err
		}

		// ---- 3. 组装订单 ----
		now := utils.NowUTC()
		expireAt := now.Add(s.settings.OrderExpireDuration())
		order := &model.Order{
			Email:          email,
			OriginalAmount: originalAmount,
			DiscountAmount: discount,
			PayAmount:      payAmount,
			CouponID:       couponID,
			CouponCode:     couponCode,
			Status:         model.OrderPending,
			DeliveryType:   product.DeliveryType,
			ClientIP:       utils.TrimAndLimit(in.ClientIP, 64),
			ExpiredAt:      &expireAt,
			QueryToken:     utils.GenerateQueryToken(),
			StockReserved:  true,
			CustomData:     customData,
		}
		if err := s.insertOrderWithUniqueNo(ctx, tx, order); err != nil {
			return err
		}

		item := model.OrderItem{
			OrderID:      order.ID,
			ProductID:    product.ID,
			ProductName:  product.Name,
			ProductSlug:  product.Slug,
			ProductCover: product.Cover,
			ProductPrice: product.Price,
			DeliveryType: product.DeliveryType,
			Quantity:     in.Quantity,
			Subtotal:     originalAmount,
		}
		if err := s.orders.CreateItems(ctx, tx, []model.OrderItem{item}); err != nil {
			return err
		}
		// CreateItems 会回填自增 ID
		items, err := s.orders.FindItems(ctx, tx, order.ID)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return errors.New("订单明细写入失败")
		}

		// ---- 4. 预占库存 ----
		if err := s.reserveStock(ctx, tx, product, order.ID, items[0].ID, in.Quantity); err != nil {
			return err
		}

		order.Items = items
		result = CreateOrderResult{Order: order, Items: items, Token: order.QueryToken}
		return nil
	})
	if err != nil {
		return nil, wrapServiceErr(err)
	}

	logger.L().Info("订单已创建",
		"order_no", result.Order.OrderNo,
		"amount", result.Order.PayAmount,
		"email", utils.MaskEmail(result.Order.Email))
	return &result, nil
}

// insertOrderWithUniqueNo 生成订单号并写入，撞唯一约束时重试。
//
// 订单号包含 10 位随机字符（31^10 ≈ 8×10^14 种组合），
// 同一秒内碰撞概率极低；重试是为了绝对可靠而非常规路径。
func (s *OrderService) insertOrderWithUniqueNo(ctx context.Context, tx *gorm.DB, order *model.Order) error {
	for attempt := 0; attempt < 5; attempt++ {
		order.ID = 0
		order.OrderNo = utils.GenerateOrderNo()
		err := s.orders.Create(ctx, tx, order)
		if err == nil {
			return nil
		}
		if !database.IsDuplicate(err) {
			return err
		}
		logger.L().Warn("订单号碰撞，重新生成", "order_no", order.OrderNo, "attempt", attempt+1)
	}
	return errors.New("生成唯一订单号失败")
}

// reserveStock 预占库存。返回业务错误表示库存不足，调用方回滚整个事务。
func (s *OrderService) reserveStock(ctx context.Context, tx *gorm.DB, product *model.Product, orderID, itemID uint64, qty int) error {
	if product.IsAuto() {
		// 自动发货：直接锁定 qty 张卡密。Claim 内部用 CAS + RowsAffected 保证不会重复分配。
		if _, err := s.codes.Claim(ctx, tx, product.ID, orderID, itemID, qty); err != nil {
			logger.L().Warn("卡密库存不足",
				"product_id", product.ID, "order_id", orderID, "quantity", qty, "err", err)
			return api.NewError(api.CodeProductOutOfStk)
		}
		return nil
	}

	// 手动发货：CAS 扣减 stock。stock=-1 表示无限库存，直接放行。
	ok, err := s.products.DeductStock(ctx, tx, product.ID, int64(qty))
	if err != nil {
		return err
	}
	if !ok {
		return api.NewError(api.CodeProductOutOfStk)
	}
	return nil
}

// releaseStock 释放订单预占的库存。必须在事务中调用。
func (s *OrderService) releaseStock(ctx context.Context, tx *gorm.DB, order *model.Order, items []model.OrderItem) error {
	if !order.StockReserved {
		return nil
	}
	if order.DeliveryType == model.DeliveryAuto {
		n, err := s.codes.Release(ctx, tx, order.ID)
		if err != nil {
			return err
		}
		logger.L().Info("已释放锁定卡密", "order_no", order.OrderNo, "count", n)
		return nil
	}
	for _, it := range items {
		if err := s.products.RestoreStock(ctx, tx, it.ProductID, int64(it.Quantity)); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 支付成功统一处理
// ---------------------------------------------------------------------------

// PaymentSuccessInput 是支付成功处理的入参。
type PaymentSuccessInput struct {
	OrderNo   string
	TradeNo   string
	Amount    int64 // 支付平台声明的实付金额（分）
	Provider  string
	ChannelID uint64
	ClientIP  string
	RawNotify string
}

// PaymentSuccessResult 是支付成功处理的结果。
type PaymentSuccessResult struct {
	Order     *model.Order
	Items     []model.OrderItem
	Duplicate bool // true = 之前已处理过，本次是重复回调
	Delivered bool // true = 本次完成了自动发货
	Attention bool // true = 需要人工介入
}

// HandlePaymentSuccess 是所有支付渠道验签成功后的**唯一**入口。
//
// 完整事务流程（对应架构文档 §23）：
//
//	开启事务 → 锁定订单行 → 幂等判断 → 金额校验 → 写支付信息 → 标记 paid
//	→ 核销优惠券 → 累加销量 → 自动发货/等待手动发货 → 写支付日志 → 提交
//	→ （事务外）异步发邮件
//
// 幂等性：订单行被锁住后再读状态，如果已经是 paid/completed 之类，
// 直接返回 Duplicate=true。支付平台推 10 次，也只会发一次货、核销一次券。
func (s *OrderService) HandlePaymentSuccess(ctx context.Context, in PaymentSuccessInput) (*PaymentSuccessResult, error) {
	var result PaymentSuccessResult

	err := s.db.Tx(ctx, func(tx *gorm.DB) error {
		// ---- 1. 锁定订单行 ----
		// 这是整个幂等机制的基石：拿到行锁之后，"读状态 → 判断 → 改状态"
		// 才是原子的。否则两个并发回调会同时读到未支付，同时发货。
		order, err := s.orders.FindByNoForUpdate(ctx, tx, in.OrderNo)
		if err != nil {
			if database.IsNotFound(err) {
				return api.NewError(api.CodeOrderNotFound)
			}
			return err
		}

		items, err := s.orders.FindItems(ctx, tx, order.ID)
		if err != nil {
			return err
		}

		// ---- 2. 幂等判断 ----
		if order.IsPaidLike() {
			result = PaymentSuccessResult{Order: order, Items: items, Duplicate: true}
			s.logPayment(ctx, tx, order, in, model.PayEventDuplicate, "已处理过的支付通知")
			logger.Payment().Info("重复的支付成功通知，已幂等跳过",
				"order_no", order.OrderNo, "status", order.Status, "trade_no", in.TradeNo)
			return nil
		}

		// ---- 2.5 渠道归属校验 ----
		// 回调必须来自这笔订单实际发起支付的那个渠道。
		//
		// 没有这一条，多渠道共存时就存在"跨渠道结算"：假设商家同时配了
		// 支付宝官方（RSA 强签名）与某个易支付站点（MD5 弱签名），
		// 一旦易支付的 key 泄露，攻击者就能用它伪造回调，
		// 把一笔本该走支付宝的订单标记为已支付 —— 弱渠道的密钥等于所有订单的密钥。
		//
		// 订单尚未选择渠道（PaymentChannelID == 0）时不作限制：
		// 那是管理员测试或线下支付补单的路径。
		if in.ChannelID > 0 && order.PaymentChannelID > 0 && in.ChannelID != order.PaymentChannelID {
			s.logPayment(ctx, tx, order, in, model.PayEventNotifyInvalid,
				fmt.Sprintf("回调渠道 %d 与订单发起支付的渠道 %d 不一致",
					in.ChannelID, order.PaymentChannelID))
			logger.Payment().Error("支付回调渠道与订单不匹配，已拒绝",
				"order_no", order.OrderNo,
				"notify_channel", in.ChannelID, "order_channel", order.PaymentChannelID)
			return api.NewErrorf(api.CodePaymentChannelMismatch,
				"回调渠道与订单发起支付的渠道不一致")
		}

		// ---- 3. 金额校验 ----
		// 严格相等，不允许任何容差。金额不符说明回调被篡改，或渠道配置错乱。
		if in.Amount != order.PayAmount {
			s.logPayment(ctx, tx, order, in, model.PayEventAmountBad,
				fmt.Sprintf("回调金额 %d 与订单应付 %d 不一致", in.Amount, order.PayAmount))
			logger.Payment().Error("支付金额不匹配",
				"order_no", order.OrderNo, "notify_amount", in.Amount, "order_amount", order.PayAmount)
			return api.NewErrorf(api.CodePaymentAmountMismatch,
				"回调金额 %s 与订单应付金额 %s 不一致",
				utils.FormatAmount(in.Amount), utils.FormatAmount(order.PayAmount))
		}

		// ---- 4. 状态流转（含过期订单复活）----
		now := utils.NowUTC()
		revived := false
		switch order.Status {
		case model.OrderPending, model.OrderPaying:
			if err := order.TransitionTo(model.OrderPaid); err != nil {
				return api.WrapError(api.CodeOrderStatusInvld, err)
			}
		case model.OrderExpired, model.OrderCancelled:
			// 「迟到的支付回调」：订单已过期，库存已释放，但钱确实收到了。
			// 绝不能吞掉这笔钱 —— 复活订单，重新尝试占用库存。
			revived = true
			logger.Payment().Warn("收到已过期/已取消订单的支付通知，尝试复活订单",
				"order_no", order.OrderNo, "status", order.Status)
			order.Status = model.OrderPaid // 恢复路径：显式绕过状态机（已在上面记日志）
			order.StockReserved = false
		default:
			return api.NewErrorf(api.CodeOrderStatusInvld,
				"订单当前状态 %s 不允许标记为已支付", order.Status)
		}

		order.PaidAt = &now
		order.PaymentTradeNo = utils.TrimAndLimit(in.TradeNo, 128)
		order.PaymentProvider = in.Provider
		if in.ChannelID > 0 {
			order.PaymentChannelID = in.ChannelID
		}

		// ---- 5. 核销优惠券 ----
		couponOK, err := s.coupons.Redeem(ctx, tx, order)
		if err != nil {
			return err
		}
		if !couponOK {
			// 券额度在下单后被抢光了。钱已经收到，绝不能失败 ——
			// 记录异常让管理员知晓，订单照常发货。
			order.NeedsAttention = true
			order.AttentionReason = appendReason(order.AttentionReason,
				"优惠券额度已耗尽，本单优惠未计入核销统计")
			logger.Payment().Warn("优惠券核销失败（额度耗尽），订单继续发货",
				"order_no", order.OrderNo, "coupon_id", order.CouponID)
		}

		// ---- 6. 复活订单需要重新占用库存 ----
		if revived {
			if err := s.reReserveForRevivedOrder(ctx, tx, order, items); err != nil {
				return err
			}
		}

		// ---- 7. 累加销量 ----
		for _, it := range items {
			if err := s.products.IncrSales(ctx, tx, it.ProductID, int64(it.Quantity)); err != nil {
				return err
			}
		}

		// ---- 8. 发货 ----
		delivered := false
		if order.DeliveryType == model.DeliveryAuto && !order.NeedsAttention {
			ok, err := s.autoDeliver(ctx, tx, order, items)
			if err != nil {
				return err
			}
			delivered = ok
		}

		switch {
		case delivered:
			if err := order.TransitionTo(model.OrderCompleted); err != nil {
				return api.WrapError(api.CodeOrderStatusInvld, err)
			}
			order.DeliveredAt = &now
		default:
			// 手动发货，或自动发货失败需要人工处理
			if err := order.TransitionTo(model.OrderWaitingDelivery); err != nil {
				return api.WrapError(api.CodeOrderStatusInvld, err)
			}
		}

		// ---- 9. 落库 ----
		fields := map[string]any{
			"status":             order.Status,
			"paid_at":            order.PaidAt,
			"payment_trade_no":   order.PaymentTradeNo,
			"payment_provider":   order.PaymentProvider,
			"payment_channel_id": order.PaymentChannelID,
			"delivery_content":   order.DeliveryContent,
			"needs_attention":    order.NeedsAttention,
			"attention_reason":   order.AttentionReason,
			"stock_reserved":     order.StockReserved,
		}
		if order.DeliveredAt != nil {
			fields["delivered_at"] = order.DeliveredAt
		}
		if err := s.orders.UpdateFields(ctx, tx, order.ID, fields); err != nil {
			return err
		}

		s.logPayment(ctx, tx, order, in, model.PayEventPaid, "支付成功并完成业务处理")

		// 重新读一次明细，带上刚写入的发货内容
		items, err = s.orders.FindItems(ctx, tx, order.ID)
		if err != nil {
			return err
		}
		order.Items = items
		result = PaymentSuccessResult{
			Order:     order,
			Items:     items,
			Delivered: delivered,
			Attention: order.NeedsAttention,
		}
		return nil
	})
	if err != nil {
		return nil, wrapServiceErr(err)
	}

	// ---- 10. 事务已提交，副作用在这之后 ----
	// 邮件失败绝不影响已提交的支付事务。
	if !result.Duplicate {
		logger.Payment().Info("支付成功处理完成",
			"order_no", result.Order.OrderNo,
			"provider", in.Provider,
			"trade_no", in.TradeNo,
			"amount", in.Amount,
			"delivered", result.Delivered,
			"attention", result.Attention)

		if result.Delivered {
			s.mailer.SendOrderMail(result.Order, result.Items, model.MailTemplateDeliver)
		} else {
			s.mailer.SendOrderMail(result.Order, result.Items, model.MailTemplatePaid)
		}

		// 商家侧通知。同样是异步旁路，失败不影响任何已提交的数据。
		if result.Attention {
			// 收了钱却发不出货 —— 最高优先级，必须让商家立刻知道
			s.notifier.NeedsAttention(result.Order, result.Order.AttentionReason)
		} else {
			// 手动发货订单会在内部升级为「待人工发货」的紧急通知
			s.notifier.OrderPaid(result.Order, result.Items)
		}
	}
	return &result, nil
}

// reReserveForRevivedOrder 为复活的过期订单重新占用库存。
//
// 抢不到库存时**不失败**：钱已经收到了。
// 改为标记 needs_attention，转人工处理（补货或退款）。
func (s *OrderService) reReserveForRevivedOrder(ctx context.Context, tx *gorm.DB, order *model.Order, items []model.OrderItem) error {
	for _, it := range items {
		product, err := s.products.FindByIDIncludeDeleted(ctx, tx, it.ProductID)
		if err != nil {
			if database.IsNotFound(err) {
				order.NeedsAttention = true
				order.AttentionReason = appendReason(order.AttentionReason,
					fmt.Sprintf("商品 #%d 已被删除，无法自动发货", it.ProductID))
				continue
			}
			return err
		}

		if it.DeliveryType == model.DeliveryAuto {
			if _, err := s.codes.Claim(ctx, tx, it.ProductID, order.ID, it.ID, it.Quantity); err != nil {
				order.NeedsAttention = true
				order.AttentionReason = appendReason(order.AttentionReason,
					fmt.Sprintf("过期订单支付回调迟到，商品「%s」卡密已售罄，需人工补货或退款", it.ProductName))
				logger.Payment().Error("复活订单时卡密不足",
					"order_no", order.OrderNo, "product_id", it.ProductID, "err", err)
				continue
			}
			order.StockReserved = true
			continue
		}

		ok, err := s.products.DeductStock(ctx, tx, product.ID, int64(it.Quantity))
		if err != nil {
			return err
		}
		if !ok {
			order.NeedsAttention = true
			order.AttentionReason = appendReason(order.AttentionReason,
				fmt.Sprintf("过期订单支付回调迟到，商品「%s」库存不足，需人工处理", it.ProductName))
			continue
		}
		order.StockReserved = true
	}
	return nil
}

// autoDeliver 完成自动发货：locked → sold，并把卡密写入订单明细。
//
// 返回 false 表示卡密数量不足，需要转人工处理。
func (s *OrderService) autoDeliver(ctx context.Context, tx *gorm.DB, order *model.Order, items []model.OrderItem) (bool, error) {
	// 先把本订单锁定的卡密标记为已售。
	// 只处理 status='locked' 的行，因此重复调用不会有副作用。
	if _, err := s.codes.MarkSold(ctx, tx, order.ID); err != nil {
		return false, err
	}

	codes, err := s.codes.FindByOrder(ctx, tx, order.ID)
	if err != nil {
		return false, err
	}

	// 按订单明细分组
	byItem := make(map[uint64][]string, len(items))
	for _, c := range codes {
		byItem[c.OrderItemID] = append(byItem[c.OrderItemID], c.Content)
	}

	var all []string
	for _, it := range items {
		if it.DeliveryType != model.DeliveryAuto {
			continue
		}
		list := byItem[it.ID]
		if len(list) < it.Quantity {
			order.NeedsAttention = true
			order.AttentionReason = appendReason(order.AttentionReason,
				fmt.Sprintf("商品「%s」应发 %d 张卡密，实际只有 %d 张，需人工补发",
					it.ProductName, it.Quantity, len(list)))
			logger.Payment().Error("自动发货卡密数量不足",
				"order_no", order.OrderNo, "item_id", it.ID,
				"need", it.Quantity, "got", len(list))
			return false, nil
		}
		content := strings.Join(list, "\n")
		if err := s.orders.UpdateItemDelivery(ctx, tx, it.ID, content); err != nil {
			return false, err
		}
		all = append(all, content)
	}

	order.DeliveryContent = strings.Join(all, "\n")
	return true, nil
}

// logPayment 写支付日志。
//
// 注意：RawNotify 由调用方（PaymentService）在传入前完成敏感字段过滤。
func (s *OrderService) logPayment(ctx context.Context, tx *gorm.DB, order *model.Order, in PaymentSuccessInput, event, status string) {
	l := &model.PaymentLog{
		OrderID:     order.ID,
		OrderNo:     order.OrderNo,
		ChannelID:   in.ChannelID,
		Provider:    in.Provider,
		TradeNo:     in.TradeNo,
		Event:       event,
		Amount:      in.Amount,
		Status:      status,
		RequestData: in.RawNotify,
		ClientIP:    in.ClientIP,
	}
	if err := s.payments.CreateLog(ctx, tx, l); err != nil {
		// 日志写失败不能让支付事务回滚 —— 钱比日志重要。
		logger.Payment().Error("写入支付日志失败", "order_no", order.OrderNo, "err", err)
	}
}

func appendReason(existing, add string) string {
	if strings.TrimSpace(existing) == "" {
		return utils.TrimAndLimit(add, 480)
	}
	return utils.TrimAndLimit(existing+"；"+add, 480)
}

// ---------------------------------------------------------------------------
// 查询
// ---------------------------------------------------------------------------

// QueryByNoAndEmail 用「订单号 + 邮箱」查询订单。
//
// IDOR 防护：订单号虽然不可枚举，仍要求配合邮箱做双因子校验；
// 邮箱比对用恒定时间比较，避免通过响应时间差侧信道爆破。
func (s *OrderService) QueryByNoAndEmail(ctx context.Context, orderNo, email string) (*model.Order, []model.OrderItem, error) {
	orderNo = strings.TrimSpace(orderNo)
	email = utils.NormalizeEmail(email)
	if orderNo == "" || email == "" {
		return nil, nil, api.NewError(api.CodeBadRequest)
	}

	order, err := s.orders.FindByNo(ctx, nil, orderNo)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, nil, api.NewError(api.CodeOrderNotFound)
		}
		return nil, nil, api.WrapError(api.CodeInternal, err)
	}
	if !utils.SecureCompare(order.Email, email) {
		// 故意返回"订单不存在"而非"邮箱错误"：
		// 区分两者等于告诉攻击者"这个订单号是真实存在的"。
		return nil, nil, api.NewError(api.CodeOrderNotFound)
	}
	return s.withItems(ctx, order)
}

// QueryByToken 用查询 Token 查订单（邮件里的免登录链接）。
func (s *OrderService) QueryByToken(ctx context.Context, token string) (*model.Order, []model.OrderItem, error) {
	token = strings.TrimSpace(token)
	if len(token) != 32 {
		return nil, nil, api.NewError(api.CodeOrderNotFound)
	}
	order, err := s.orders.FindByToken(ctx, nil, token)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, nil, api.NewError(api.CodeOrderNotFound)
		}
		return nil, nil, api.WrapError(api.CodeInternal, err)
	}
	return s.withItems(ctx, order)
}

// GetByNo 按订单号查询（内部/后台使用，不做归属校验）。
func (s *OrderService) GetByNo(ctx context.Context, orderNo string) (*model.Order, []model.OrderItem, error) {
	order, err := s.orders.FindByNo(ctx, nil, orderNo)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, nil, api.NewError(api.CodeOrderNotFound)
		}
		return nil, nil, api.WrapError(api.CodeInternal, err)
	}
	return s.withItems(ctx, order)
}

// GetByID 按 ID 查询（后台使用）。
func (s *OrderService) GetByID(ctx context.Context, id uint64) (*model.Order, []model.OrderItem, error) {
	order, err := s.orders.FindByID(ctx, nil, id)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, nil, api.NewError(api.CodeOrderNotFound)
		}
		return nil, nil, api.WrapError(api.CodeInternal, err)
	}
	return s.withItems(ctx, order)
}

func (s *OrderService) withItems(ctx context.Context, order *model.Order) (*model.Order, []model.OrderItem, error) {
	items, err := s.orders.FindItems(ctx, nil, order.ID)
	if err != nil {
		return nil, nil, api.WrapError(api.CodeInternal, err)
	}
	order.Items = items
	return order, items, nil
}

// List 分页查询订单（后台）。
func (s *OrderService) List(ctx context.Context, q repository.OrderQuery) ([]model.Order, int64, error) {
	list, total, err := s.orders.List(ctx, nil, q)
	if err != nil {
		return nil, 0, api.WrapError(api.CodeInternal, err)
	}
	if len(list) == 0 {
		return list, total, nil
	}
	ids := make([]uint64, 0, len(list))
	for _, o := range list {
		ids = append(ids, o.ID)
	}
	itemMap, err := s.orders.FindItemsBatch(ctx, nil, ids)
	if err != nil {
		return nil, 0, api.WrapError(api.CodeInternal, err)
	}
	for i := range list {
		list[i].Items = itemMap[list[i].ID]
	}
	return list, total, nil
}

// ---------------------------------------------------------------------------
// 状态变更
// ---------------------------------------------------------------------------

// MarkPaying 在跳转支付前把订单标记为支付中。
func (s *OrderService) MarkPaying(ctx context.Context, orderID uint64, channelID uint64, method, provider string) error {
	return wrapServiceErr(s.db.Tx(ctx, func(tx *gorm.DB) error {
		order, err := s.orders.FindByIDForUpdate(ctx, tx, orderID)
		if err != nil {
			return err
		}
		if order.Status != model.OrderPending && order.Status != model.OrderPaying {
			return api.NewError(api.CodeOrderStatusInvld)
		}
		fields := map[string]any{
			"payment_channel_id": channelID,
			"payment_method":     method,
			"payment_provider":   provider,
		}
		if order.Status == model.OrderPending {
			fields["status"] = model.OrderPaying
		}
		return s.orders.UpdateFields(ctx, tx, orderID, fields)
	}))
}

// Cancel 取消未支付订单并释放库存。
func (s *OrderService) Cancel(ctx context.Context, orderNo, email string) error {
	email = utils.NormalizeEmail(email)
	return wrapServiceErr(s.db.Tx(ctx, func(tx *gorm.DB) error {
		order, err := s.orders.FindByNoForUpdate(ctx, tx, orderNo)
		if err != nil {
			if database.IsNotFound(err) {
				return api.NewError(api.CodeOrderNotFound)
			}
			return err
		}
		if email != "" && !utils.SecureCompare(order.Email, email) {
			return api.NewError(api.CodeOrderNotFound)
		}
		if !order.IsPayable() {
			return api.NewErrorf(api.CodeOrderStatusInvld, "当前状态（%s）不允许取消", model.OrderStatusLabel(order.Status))
		}

		items, err := s.orders.FindItems(ctx, tx, order.ID)
		if err != nil {
			return err
		}
		if err := s.releaseStock(ctx, tx, order, items); err != nil {
			return err
		}
		if err := order.TransitionTo(model.OrderCancelled); err != nil {
			return api.WrapError(api.CodeOrderStatusInvld, err)
		}
		return s.orders.UpdateFields(ctx, tx, order.ID, map[string]any{
			"status":         order.Status,
			"stock_reserved": false,
		})
	}))
}

// ManualDeliver 管理员手动发货。
func (s *OrderService) ManualDeliver(ctx context.Context, orderID uint64, content string) (*model.Order, []model.OrderItem, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, nil, api.NewErrorf(api.CodeValidation, "发货内容不能为空")
	}
	if len([]rune(content)) > 20000 {
		return nil, nil, api.NewErrorf(api.CodeValidation, "发货内容过长")
	}

	var (
		order *model.Order
		items []model.OrderItem
	)
	err := s.db.Tx(ctx, func(tx *gorm.DB) error {
		o, err := s.orders.FindByIDForUpdate(ctx, tx, orderID)
		if err != nil {
			if database.IsNotFound(err) {
				return api.NewError(api.CodeOrderNotFound)
			}
			return err
		}
		if o.Status != model.OrderWaitingDelivery && o.Status != model.OrderPaid {
			return api.NewErrorf(api.CodeOrderStatusInvld,
				"只有已支付/待发货的订单才能发货，当前状态：%s", model.OrderStatusLabel(o.Status))
		}

		its, err := s.orders.FindItems(ctx, tx, o.ID)
		if err != nil {
			return err
		}
		for _, it := range its {
			if err := s.orders.UpdateItemDelivery(ctx, tx, it.ID, content); err != nil {
				return err
			}
		}

		now := utils.NowUTC()
		if err := o.TransitionTo(model.OrderCompleted); err != nil {
			return api.WrapError(api.CodeOrderStatusInvld, err)
		}
		o.DeliveryContent = content
		o.DeliveredAt = &now
		// 手动发货完成后清除异常标记
		o.NeedsAttention = false

		if err := s.orders.UpdateFields(ctx, tx, o.ID, map[string]any{
			"status":           o.Status,
			"delivery_content": content,
			"delivered_at":     now,
			"needs_attention":  false,
		}); err != nil {
			return err
		}

		its, err = s.orders.FindItems(ctx, tx, o.ID)
		if err != nil {
			return err
		}
		o.Items = its
		order, items = o, its
		return nil
	})
	if err != nil {
		return nil, nil, wrapServiceErr(err)
	}

	s.mailer.SendOrderMail(order, items, model.MailTemplateManual)
	return order, items, nil
}

// AddRemark 添加订单备注。
func (s *OrderService) AddRemark(ctx context.Context, orderID uint64, remark string) error {
	return wrapServiceErr(s.orders.UpdateFields(ctx, nil, orderID, map[string]any{
		"remark": utils.TrimAndLimit(remark, 2000),
	}))
}

// ClearAttention 清除订单的人工处理标记。
func (s *OrderService) ClearAttention(ctx context.Context, orderID uint64) error {
	return wrapServiceErr(s.orders.UpdateFields(ctx, nil, orderID, map[string]any{
		"needs_attention":  false,
		"attention_reason": "",
	}))
}

// RefundInput 是退款入参。
type RefundInput struct {
	Amount int64  `json:"amount"`
	Reason string `json:"reason"`
	Manual bool   `json:"manual"` // true = 仅标记为人工退款，不调用支付渠道
}

// MarkRefunded 把订单标记为已退款并归还库存/优惠券额度。
//
// 实际的资金退回由 PaymentService 负责（可能调用渠道接口，也可能是人工线下退）。
// 这里只处理商城侧的状态与账目。
func (s *OrderService) MarkRefunded(ctx context.Context, orderID uint64, amount int64, reason string) (*model.Order, error) {
	var order *model.Order
	err := s.db.Tx(ctx, func(tx *gorm.DB) error {
		o, err := s.orders.FindByIDForUpdate(ctx, tx, orderID)
		if err != nil {
			if database.IsNotFound(err) {
				return api.NewError(api.CodeOrderNotFound)
			}
			return err
		}
		if !o.IsPaidLike() || o.Status == model.OrderRefunded {
			return api.NewErrorf(api.CodeOrderStatusInvld,
				"当前状态（%s）不允许退款", model.OrderStatusLabel(o.Status))
		}
		if amount <= 0 || amount > o.PayAmount {
			return api.NewErrorf(api.CodeValidation,
				"退款金额必须在 0 - %s 之间", utils.FormatAmount(o.PayAmount))
		}

		// 归还优惠券额度（只有全额退款才归还；部分退款保留核销记录）
		if amount == o.PayAmount {
			if err := s.coupons.ReleaseRedemption(ctx, tx, o); err != nil {
				return err
			}
		}

		now := utils.NowUTC()
		if err := o.TransitionTo(model.OrderRefunded); err != nil {
			return api.WrapError(api.CodeOrderStatusInvld, err)
		}
		if err := s.orders.UpdateFields(ctx, tx, o.ID, map[string]any{
			"status":        o.Status,
			"refund_amount": amount,
			"refund_reason": utils.TrimAndLimit(reason, 480),
			"refunded_at":   now,
		}); err != nil {
			return err
		}
		o.RefundAmount = amount
		o.RefundReason = reason
		o.RefundedAt = &now
		order = o
		return nil
	})
	if err != nil {
		return nil, wrapServiceErr(err)
	}
	logger.L().Info("订单已退款", "order_no", order.OrderNo, "amount", amount, "reason", reason)
	return order, nil
}

// ---------------------------------------------------------------------------
// 订单过期
// ---------------------------------------------------------------------------

// ExpireDueOrders 把超时未支付的订单置为已过期并释放库存。
//
// 由定时任务调用。每次最多处理 batch 条，避免单次事务过大。
func (s *OrderService) ExpireDueOrders(ctx context.Context, batch int) (int, error) {
	if batch <= 0 {
		batch = 100
	}
	now := utils.NowUTC()

	due, err := s.orders.FindExpiring(ctx, nil, now, batch)
	if err != nil {
		return 0, err
	}
	if len(due) == 0 {
		return 0, nil
	}

	expired := 0
	for _, o := range due {
		orderID := o.ID
		err := s.db.Tx(ctx, func(tx *gorm.DB) error {
			// 事务内重新加锁读取：从扫描到处理之间订单可能已被支付
			order, err := s.orders.FindByIDForUpdate(ctx, tx, orderID)
			if err != nil {
				return err
			}
			if !order.IsPayable() || !order.IsExpiredAt(now) {
				return nil // 已被支付或已被处理，跳过
			}

			items, err := s.orders.FindItems(ctx, tx, order.ID)
			if err != nil {
				return err
			}
			if err := s.releaseStock(ctx, tx, order, items); err != nil {
				return err
			}
			if err := order.TransitionTo(model.OrderExpired); err != nil {
				return err
			}
			if err := s.orders.UpdateFields(ctx, tx, order.ID, map[string]any{
				"status":         order.Status,
				"stock_reserved": false,
			}); err != nil {
				return err
			}
			expired++
			return nil
		})
		if err != nil {
			logger.L().Error("处理过期订单失败", "order_id", orderID, "err", err)
		}
	}

	if expired > 0 {
		logger.L().Info("已处理过期订单", "count", expired)
	}
	return expired, nil
}

// ReleaseStaleCodeLocks 释放孤儿卡密锁定（进程崩溃等异常留下的）。
func (s *OrderService) ReleaseStaleCodeLocks(ctx context.Context, olderThan time.Duration) (int64, error) {
	before := utils.NowUTC().Add(-olderThan)
	var n int64
	err := s.db.Tx(ctx, func(tx *gorm.DB) error {
		var err error
		n, err = s.codes.ReleaseStaleLocks(ctx, tx, before)
		return err
	})
	if n > 0 {
		logger.L().Warn("已释放孤儿卡密锁定", "count", n)
	}
	return n, err
}
