// Package model 定义数据库实体与领域常量。
//
// 约定：
//   - 金额一律 int64，单位为最小货币单位（人民币 = 分）。绝不使用 float。
//   - 时间一律 time.Time 且以 UTC 存储；展示时区在 handler/前端转换。
//   - 状态一律用字符串常量，并配合状态机做合法性校验。
package model

import "time"

// Model 是通用主键 + 时间戳。不使用 gorm.Model，因为它带 DeletedAt，
// 而绝大多数表（订单、支付记录）明确禁止软删。
type Model struct {
	ID        uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at"`
}

// 通用启用/禁用状态。
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// AllTables 返回全部表名，供健康检查与测试建表使用。
func AllTables() []string {
	return []string{
		"admins", "categories", "products", "product_codes",
		"orders", "order_items", "coupons", "coupon_products", "coupon_usages",
		"payment_channels", "payment_logs", "system_settings",
		"admin_operation_logs", "email_logs",
	}
}
