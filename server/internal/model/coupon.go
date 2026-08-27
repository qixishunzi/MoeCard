package model

import "time"

// 优惠券类型。
const (
	CouponFixed   = "fixed"   // 固定金额优惠，Value 单位 = 分
	CouponPercent = "percent" // 百分比优惠，Value = 万分比（9 折 = 9000）
)

// 优惠券适用范围。
const (
	CouponScopeAll      = "all"
	CouponScopeProducts = "products"
)

// PercentBase 是百分比优惠的基数。
//
// 为什么用万分比：`9 折` 若存 0.9 就引入浮点。存 9000 后，
// discount = amount * (10000-9000) / 10000 全程整数运算，结果确定可测试。
const PercentBase int64 = 10000

// Coupon 是优惠券。
type Coupon struct {
	Model
	Code         string `gorm:"column:code;size:64;uniqueIndex" json:"code"`
	Name         string `gorm:"column:name;size:191" json:"name"`
	Type         string `gorm:"column:type;size:16" json:"type"`
	Value        int64  `gorm:"column:value" json:"value"`
	MinAmount    int64  `gorm:"column:min_amount" json:"min_amount"`
	MaxDiscount  int64  `gorm:"column:max_discount" json:"max_discount"`
	Scope        string `gorm:"column:scope;size:16" json:"scope"`
	UsageLimit   int64  `gorm:"column:usage_limit" json:"usage_limit"`
	UsedCount    int64  `gorm:"column:used_count" json:"used_count"`
	PerUserLimit int64  `gorm:"column:per_user_limit" json:"per_user_limit"`

	StartAt  *time.Time `gorm:"column:start_at" json:"start_at"`
	ExpireAt *time.Time `gorm:"column:expire_at" json:"expire_at"`
	Status   string     `gorm:"column:status;size:16" json:"status"`

	// 非数据库字段：scope=products 时的关联商品
	ProductIDs []uint64  `gorm:"-" json:"product_ids,omitempty"`
	Products   []Product `gorm:"-" json:"products,omitempty"`
}

func (Coupon) TableName() string { return "coupons" }

// CalcDiscount 计算给定原价下的优惠金额（分）。全程整数运算。
//
// 规则：
//   - fixed   : discount = Value
//   - percent : discount = amount * (10000 - Value) / 10000，再受 MaxDiscount 限制
//   - 结果 clamp 到 [0, amount]，保证折后金额永不为负。
func (c *Coupon) CalcDiscount(amount int64) int64 {
	if amount <= 0 {
		return 0
	}
	var d int64
	switch c.Type {
	case CouponFixed:
		d = c.Value
	case CouponPercent:
		v := c.Value
		if v < 0 {
			v = 0
		}
		if v > PercentBase {
			v = PercentBase
		}
		d = amount * (PercentBase - v) / PercentBase
		if c.MaxDiscount > 0 && d > c.MaxDiscount {
			d = c.MaxDiscount
		}
	default:
		return 0
	}
	if d < 0 {
		d = 0
	}
	if d > amount {
		d = amount
	}
	return d
}

// HasQuota 判断总使用次数是否还有余量。
func (c *Coupon) HasQuota() bool {
	return c.UsageLimit == 0 || c.UsedCount < c.UsageLimit
}

// CouponProduct 是优惠券与商品的多对多关联。
type CouponProduct struct {
	ID        uint64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	CouponID  uint64 `gorm:"column:coupon_id;index" json:"coupon_id"`
	ProductID uint64 `gorm:"column:product_id;index" json:"product_id"`
}

func (CouponProduct) TableName() string { return "coupon_products" }

// CouponUsage 记录一次真实核销（只在支付成功后写入）。
//
// (coupon_id, order_id) 唯一约束是幂等的核心：
// 同一订单的重复支付回调第二次插入会直接冲突，从而保证只核销一次。
type CouponUsage struct {
	ID             uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	CouponID       uint64    `gorm:"column:coupon_id;index" json:"coupon_id"`
	OrderID        uint64    `gorm:"column:order_id" json:"order_id"`
	OrderNo        string    `gorm:"column:order_no;size:40" json:"order_no"`
	Email          string    `gorm:"column:email;size:190;index" json:"email"`
	DiscountAmount int64     `gorm:"column:discount_amount" json:"discount_amount"`
	CreatedAt      time.Time `gorm:"column:created_at" json:"created_at"`
}

func (CouponUsage) TableName() string { return "coupon_usages" }
