package model

import (
	"fmt"
	"time"
)

// 订单状态。
const (
	OrderPending         = "pending"          // 待付款（已预占库存）
	OrderPaying          = "paying"           // 已跳转支付，等待回调
	OrderPaid            = "paid"             // 已支付
	OrderWaitingDelivery = "waiting_delivery" // 已支付，等待管理员手动发货
	OrderCompleted       = "completed"        // 已完成
	OrderCancelled       = "cancelled"        // 用户/管理员取消
	OrderExpired         = "expired"          // 超时未支付
	OrderRefunded        = "refunded"         // 已退款
)

// orderTransitions 是订单状态机的合法转换表。
//
// 这是防止"已完成订单被改回待付款"这类灾难的唯一防线。
// 任何状态变更都必须经过 Order.TransitionTo，禁止直接赋值 o.Status。
var orderTransitions = map[string]map[string]bool{
	OrderPending: {
		OrderPaying:    true,
		OrderPaid:      true,
		OrderCancelled: true,
		OrderExpired:   true,
	},
	OrderPaying: {
		OrderPending:   true, // 用户放弃支付回到待付款
		OrderPaid:      true,
		OrderCancelled: true,
		OrderExpired:   true,
	},
	OrderPaid: {
		OrderWaitingDelivery: true,
		OrderCompleted:       true,
		OrderRefunded:        true,
	},
	OrderWaitingDelivery: {
		OrderCompleted: true,
		OrderRefunded:  true,
	},
	OrderCompleted: {
		OrderRefunded: true,
	},
	// 终态：无出边
	OrderCancelled: {},
	OrderExpired:   {},
	OrderRefunded:  {},
}

// orderStatusLabels 提供中文文案，供后台与邮件模板使用。
var orderStatusLabels = map[string]string{
	OrderPending:         "待付款",
	OrderPaying:          "支付中",
	OrderPaid:            "已支付",
	OrderWaitingDelivery: "待发货",
	OrderCompleted:       "已完成",
	OrderCancelled:       "已取消",
	OrderExpired:         "已过期",
	OrderRefunded:        "已退款",
}

// OrderStatusLabel 返回状态中文文案。
func OrderStatusLabel(s string) string {
	if l, ok := orderStatusLabels[s]; ok {
		return l
	}
	return s
}

// IsValidOrderStatus 判断状态字符串是否合法。
func IsValidOrderStatus(s string) bool {
	_, ok := orderTransitions[s]
	return ok
}

// CanTransition 判断状态转换是否合法。
func CanTransition(from, to string) bool {
	if from == to {
		return false
	}
	next, ok := orderTransitions[from]
	if !ok {
		return false
	}
	return next[to]
}

// Order 是订单主表。
//
// 设计要点：
//   - 商品信息不放在这里，而是放在 order_items 的快照字段中（为购物车预留）。
//   - QueryToken 让用户无需邮箱即可查单，同时避免 order_no 被遍历（IDOR 防护）。
//   - StockReserved 标记是否已预占库存，决定过期/取消时是否需要释放。
//   - NeedsAttention 标记需要人工介入的异常订单（如迟到回调时已无库存）。
type Order struct {
	Model
	OrderNo    string `gorm:"column:order_no;size:40;uniqueIndex" json:"order_no"`
	QueryToken string `gorm:"column:query_token;size:32;uniqueIndex" json:"-"`
	Email      string `gorm:"column:email;size:190;index" json:"email"`

	OriginalAmount int64  `gorm:"column:original_amount" json:"original_amount"`
	DiscountAmount int64  `gorm:"column:discount_amount" json:"discount_amount"`
	PayAmount      int64  `gorm:"column:pay_amount" json:"pay_amount"`
	CouponID       uint64 `gorm:"column:coupon_id" json:"coupon_id"`
	CouponCode     string `gorm:"column:coupon_code;size:64" json:"coupon_code"`

	PaymentChannelID uint64 `gorm:"column:payment_channel_id" json:"payment_channel_id"`
	PaymentMethod    string `gorm:"column:payment_method;size:64" json:"payment_method"`
	PaymentProvider  string `gorm:"column:payment_provider;size:32" json:"payment_provider"`
	PaymentTradeNo   string `gorm:"column:payment_trade_no;size:128" json:"payment_trade_no"`

	Status          string `gorm:"column:status;size:24;index" json:"status"`
	DeliveryType    string `gorm:"column:delivery_type;size:16" json:"delivery_type"`
	DeliveryContent string `gorm:"column:delivery_content" json:"delivery_content"`

	StockReserved   bool   `gorm:"column:stock_reserved" json:"-"`
	NeedsAttention  bool   `gorm:"column:needs_attention" json:"needs_attention"`
	AttentionReason string `gorm:"column:attention_reason;size:500" json:"attention_reason"`
	Remark          string `gorm:"column:remark" json:"remark"`
	// CustomData 是买家下单时填写的自定义信息（JSON 对象：字段 key -> 值）。
	CustomData string `gorm:"column:custom_data" json:"-"`
	ClientIP   string `gorm:"column:client_ip;size:64" json:"-"`

	RefundAmount int64      `gorm:"column:refund_amount" json:"refund_amount"`
	RefundReason string     `gorm:"column:refund_reason;size:500" json:"refund_reason"`
	RefundedAt   *time.Time `gorm:"column:refunded_at" json:"refunded_at"`

	PaidAt      *time.Time `gorm:"column:paid_at" json:"paid_at"`
	DeliveredAt *time.Time `gorm:"column:delivered_at" json:"delivered_at"`
	ExpiredAt   *time.Time `gorm:"column:expired_at" json:"expired_at"`

	// 非数据库字段
	Items []OrderItem `gorm:"-" json:"items,omitempty"`
}

func (Order) TableName() string { return "orders" }

// TransitionTo 校验并执行状态转换。
// 返回错误说明该转换非法 —— 调用方必须处理，禁止忽略。
func (o *Order) TransitionTo(next string) error {
	if !IsValidOrderStatus(next) {
		return fmt.Errorf("unknown order status %q", next)
	}
	if !CanTransition(o.Status, next) {
		return fmt.Errorf("illegal order status transition: %s -> %s", o.Status, next)
	}
	o.Status = next
	return nil
}

// IsPaidLike 判断订单是否已经收到钱（用于支付回调幂等判断）。
func (o *Order) IsPaidLike() bool {
	switch o.Status {
	case OrderPaid, OrderWaitingDelivery, OrderCompleted, OrderRefunded:
		return true
	}
	return false
}

// IsFinal 判断订单是否处于终态。
func (o *Order) IsFinal() bool {
	switch o.Status {
	case OrderCompleted, OrderCancelled, OrderExpired, OrderRefunded:
		return true
	}
	return false
}

// IsPayable 判断订单当前是否还能发起支付。
func (o *Order) IsPayable() bool {
	return o.Status == OrderPending || o.Status == OrderPaying
}

// IsExpiredAt 判断订单在给定时刻是否已超时。
func (o *Order) IsExpiredAt(t time.Time) bool {
	return o.ExpiredAt != nil && t.After(*o.ExpiredAt)
}

// OrderItem 是订单明细，保存下单时的商品快照。
//
// 为什么必须快照：商品可能被改名、改价、软删。
// 只存 product_id 的话，历史订单会在商品变更后显示错误信息。
type OrderItem struct {
	ID              uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	OrderID         uint64    `gorm:"column:order_id;index" json:"order_id"`
	ProductID       uint64    `gorm:"column:product_id" json:"product_id"`
	ProductName     string    `gorm:"column:product_name;size:191" json:"product_name"`
	ProductSlug     string    `gorm:"column:product_slug;size:150" json:"product_slug"`
	ProductCover    string    `gorm:"column:product_cover;size:500" json:"product_cover"`
	ProductPrice    int64     `gorm:"column:product_price" json:"product_price"`
	DeliveryType    string    `gorm:"column:delivery_type;size:16" json:"delivery_type"`
	Quantity        int       `gorm:"column:quantity" json:"quantity"`
	Subtotal        int64     `gorm:"column:subtotal" json:"subtotal"`
	DeliveryContent string    `gorm:"column:delivery_content" json:"delivery_content"`
	CreatedAt       time.Time `gorm:"column:created_at" json:"created_at"`
}

func (OrderItem) TableName() string { return "order_items" }
