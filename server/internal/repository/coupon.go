package repository

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/utils"
)

// CouponRepo 是优惠券仓储。
type CouponRepo struct{ Base }

func (r *CouponRepo) FindByID(ctx context.Context, tx *gorm.DB, id uint64) (*model.Coupon, error) {
	var c model.Coupon
	if err := r.conn(ctx, tx).First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// FindByCode 按券码查询。券码统一大写存储与比对，避免大小写导致"券码无效"。
func (r *CouponRepo) FindByCode(ctx context.Context, tx *gorm.DB, code string) (*model.Coupon, error) {
	var c model.Coupon
	code = strings.ToUpper(strings.TrimSpace(code))
	if err := r.conn(ctx, tx).Where("code = ?", code).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *CouponRepo) Create(ctx context.Context, tx *gorm.DB, c *model.Coupon) error {
	now := utils.NowUTC()
	c.CreatedAt, c.UpdatedAt = now, now
	c.Code = strings.ToUpper(strings.TrimSpace(c.Code))
	return r.conn(ctx, tx).Create(c).Error
}

func (r *CouponRepo) Update(ctx context.Context, tx *gorm.DB, c *model.Coupon) error {
	c.UpdatedAt = utils.NowUTC()
	c.Code = strings.ToUpper(strings.TrimSpace(c.Code))
	return r.conn(ctx, tx).Model(&model.Coupon{}).Where("id = ?", c.ID).
		Select("code", "name", "type", "value", "min_amount", "max_discount", "scope",
			"usage_limit", "per_user_limit", "start_at", "expire_at", "status", "updated_at").
		Updates(c).Error
}

func (r *CouponRepo) Delete(ctx context.Context, tx *gorm.DB, id uint64) error {
	if err := r.conn(ctx, tx).Where("coupon_id = ?", id).Delete(&model.CouponProduct{}).Error; err != nil {
		return err
	}
	// coupon_usages 保留：那是历史核销记录，删掉会让订单的优惠来源无从追溯。
	return r.conn(ctx, tx).Delete(&model.Coupon{}, id).Error
}

func (r *CouponRepo) CodeExists(ctx context.Context, tx *gorm.DB, code string, excludeID uint64) (bool, error) {
	db := r.conn(ctx, tx).Model(&model.Coupon{}).
		Where("code = ?", strings.ToUpper(strings.TrimSpace(code)))
	if excludeID > 0 {
		db = db.Where("id <> ?", excludeID)
	}
	var n int64
	err := db.Count(&n).Error
	return n > 0, err
}

// CouponQuery 是优惠券列表查询条件。
type CouponQuery struct {
	Keyword string
	Type    string
	Status  string
	Scope   string
	Offset  int
	Limit   int
}

func (r *CouponRepo) List(ctx context.Context, tx *gorm.DB, q CouponQuery) ([]model.Coupon, int64, error) {
	db := r.conn(ctx, tx).Model(&model.Coupon{})
	if q.Keyword != "" {
		kw := "%" + escapeLike(q.Keyword) + "%"
		db = db.Where("code LIKE ? OR name LIKE ?", kw, kw)
	}
	if q.Type != "" {
		db = db.Where("type = ?", q.Type)
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.Scope != "" {
		db = db.Where("scope = ?", q.Scope)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []model.Coupon
	err := db.Order("id DESC").Offset(q.Offset).Limit(q.Limit).Find(&out).Error
	return out, total, err
}

// ---- 优惠券 ↔ 商品 多对多 ----

// ReplaceProducts 全量替换优惠券的适用商品。必须在事务中调用。
func (r *CouponRepo) ReplaceProducts(ctx context.Context, tx *gorm.DB, couponID uint64, productIDs []uint64) error {
	db := r.conn(ctx, tx)
	if err := db.Where("coupon_id = ?", couponID).Delete(&model.CouponProduct{}).Error; err != nil {
		return err
	}
	productIDs = utils.DedupeUint64(productIDs)
	if len(productIDs) == 0 {
		return nil
	}
	rows := make([]model.CouponProduct, 0, len(productIDs))
	for _, pid := range productIDs {
		rows = append(rows, model.CouponProduct{CouponID: couponID, ProductID: pid})
	}
	return db.CreateInBatches(&rows, 200).Error
}

// ProductIDs 返回优惠券关联的商品 ID。
func (r *CouponRepo) ProductIDs(ctx context.Context, tx *gorm.DB, couponID uint64) ([]uint64, error) {
	var ids []uint64
	err := r.conn(ctx, tx).Model(&model.CouponProduct{}).
		Where("coupon_id = ?", couponID).Pluck("product_id", &ids).Error
	return ids, err
}

// AppliesToProduct 判断优惠券是否适用于指定商品。
func (r *CouponRepo) AppliesToProduct(ctx context.Context, tx *gorm.DB, couponID, productID uint64) (bool, error) {
	var n int64
	err := r.conn(ctx, tx).Model(&model.CouponProduct{}).
		Where("coupon_id = ? AND product_id = ?", couponID, productID).
		Limit(1).Count(&n).Error
	return n > 0, err
}

// ---- 核销 ----

// TryConsume 以 CAS 方式占用一次优惠券额度。
//
// UPDATE ... WHERE usage_limit = 0 OR used_count < usage_limit
// 保证并发下不会超发：只有真正把 used_count 加上去的事务才返回 true。
//
// 必须在支付成功事务内调用 —— 在创建订单时扣减会让用户通过
// 狂建未支付订单把优惠券额度耗光。
func (r *CouponRepo) TryConsume(ctx context.Context, tx *gorm.DB, couponID uint64) (bool, error) {
	res := r.conn(ctx, tx).Model(&model.Coupon{}).
		Where("id = ? AND (usage_limit = 0 OR used_count < usage_limit)", couponID).
		UpdateColumn("used_count", gorm.Expr("used_count + 1"))
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// ReleaseConsume 归还一次额度（退款时使用）。
func (r *CouponRepo) ReleaseConsume(ctx context.Context, tx *gorm.DB, couponID uint64) error {
	return r.conn(ctx, tx).Model(&model.Coupon{}).
		Where("id = ? AND used_count > 0", couponID).
		UpdateColumn("used_count", gorm.Expr("used_count - 1")).Error
}

// CreateUsage 写入核销记录。
//
// (coupon_id, order_id) 唯一约束：重复的支付回调第二次插入会冲突，
// 调用方据此判定"已核销过"，从而实现幂等。
func (r *CouponRepo) CreateUsage(ctx context.Context, tx *gorm.DB, u *model.CouponUsage) error {
	u.CreatedAt = utils.NowUTC()
	u.Email = utils.NormalizeEmail(u.Email)
	return r.conn(ctx, tx).Create(u).Error
}

// UsageExists 判断某订单是否已核销过该优惠券。
func (r *CouponRepo) UsageExists(ctx context.Context, tx *gorm.DB, couponID, orderID uint64) (bool, error) {
	var n int64
	err := r.conn(ctx, tx).Model(&model.CouponUsage{}).
		Where("coupon_id = ? AND order_id = ?", couponID, orderID).
		Limit(1).Count(&n).Error
	return n > 0, err
}

// CountUsageByEmail 统计某邮箱已核销该优惠券的次数（per_user_limit 用）。
func (r *CouponRepo) CountUsageByEmail(ctx context.Context, tx *gorm.DB, couponID uint64, email string) (int64, error) {
	var n int64
	err := r.conn(ctx, tx).Model(&model.CouponUsage{}).
		Where("coupon_id = ? AND email = ?", couponID, utils.NormalizeEmail(email)).
		Count(&n).Error
	return n, err
}

// ListUsages 查询优惠券核销记录。
func (r *CouponRepo) ListUsages(ctx context.Context, tx *gorm.DB, couponID uint64, offset, limit int) ([]model.CouponUsage, int64, error) {
	db := r.conn(ctx, tx).Model(&model.CouponUsage{}).Where("coupon_id = ?", couponID)
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []model.CouponUsage
	err := db.Order("id DESC").Offset(offset).Limit(limit).Find(&out).Error
	return out, total, err
}

// DeleteUsageByOrder 删除某订单的核销记录（退款回滚时使用）。
func (r *CouponRepo) DeleteUsageByOrder(ctx context.Context, tx *gorm.DB, couponID, orderID uint64) error {
	return r.conn(ctx, tx).
		Where("coupon_id = ? AND order_id = ?", couponID, orderID).
		Delete(&model.CouponUsage{}).Error
}
