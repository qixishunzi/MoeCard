// Package repository 是数据访问层。
//
// 规则：
//   - 只做 CRUD 与查询，不开启事务（事务边界属于 service）。
//   - 每个方法第一个参数是 tx *gorm.DB：传 nil 用默认连接，
//     传事务句柄则加入调用方的事务。这样同一份代码在事务内外都能复用。
//   - 不调用其他 service，不产生副作用（发邮件、写日志等）。
package repository

import (
	"context"

	"gorm.io/gorm"

	"github.com/moecard/server/internal/database"
)

// Base 提供连接选择能力。
type Base struct {
	db *database.DB
}

// NewBase 构造。
func NewBase(db *database.DB) Base { return Base{db: db} }

// DB 返回底层封装（service 需要开事务时使用）。
func (b Base) DB() *database.DB { return b.db }

// conn 选择连接：优先使用调用方传入的事务句柄。
func (b Base) conn(ctx context.Context, tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx.WithContext(ctx)
	}
	return b.db.DB.WithContext(ctx)
}

// Repositories 聚合所有仓储，便于依赖注入。
type Repositories struct {
	Admin    *AdminRepo
	Category *CategoryRepo
	Product  *ProductRepo
	Code     *CodeRepo
	Order    *OrderRepo
	Coupon   *CouponRepo
	Payment  *PaymentRepo
	Setting  *SettingRepo
	Log      *LogRepo
	Notify   *NotifyRepo
}

// New 构造全部仓储。
func New(db *database.DB) *Repositories {
	base := NewBase(db)
	return &Repositories{
		Admin:    &AdminRepo{base},
		Category: &CategoryRepo{base},
		Product:  &ProductRepo{base},
		Code:     &CodeRepo{base},
		Order:    &OrderRepo{base},
		Coupon:   &CouponRepo{base},
		Payment:  &PaymentRepo{base},
		Setting:  &SettingRepo{base},
		Log:      &LogRepo{base},
		Notify:   &NotifyRepo{base},
	}
}
