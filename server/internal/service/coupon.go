package service

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/database"
	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/utils"
)

// CouponService 处理优惠券校验、计算与核销。
type CouponService struct {
	db      *database.DB
	repo    *repository.CouponRepo
	product *repository.ProductRepo
}

// NewCouponService 构造。
func NewCouponService(db *database.DB, repo *repository.CouponRepo, product *repository.ProductRepo) *CouponService {
	return &CouponService{db: db, repo: repo, product: product}
}

// DiscountResult 是优惠试算结果。
type DiscountResult struct {
	Coupon         *model.Coupon `json:"-"`
	CouponID       uint64        `json:"coupon_id"`
	CouponCode     string        `json:"coupon_code"`
	CouponName     string        `json:"coupon_name"`
	OriginalAmount int64         `json:"original_amount"`
	DiscountAmount int64         `json:"discount_amount"`
	PayAmount      int64         `json:"pay_amount"`
}

// Validate 校验优惠券并计算优惠金额。
//
// 完整校验链（顺序即错误提示的优先级）：
//  1. 存在      2. 启用      3. 已开始    4. 未过期
//  5. 有剩余次数 6. 适用商品  7. 满足门槛  8. 未超个人限次
//
// **只做校验与计算，不扣减 used_count** ——
// 扣减发生在支付成功事务内，否则用户狂建未支付订单就能把券刷光。
func (s *CouponService) Validate(ctx context.Context, tx *gorm.DB, code string, productID uint64, originalAmount int64, email string) (*DiscountResult, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, api.NewError(api.CodeCouponInvalid)
	}

	c, err := s.repo.FindByCode(ctx, tx, code)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, api.NewError(api.CodeCouponInvalid)
		}
		return nil, api.WrapError(api.CodeInternal, err)
	}

	if c.Status != model.StatusActive {
		return nil, api.NewError(api.CodeCouponDisabled)
	}

	now := utils.NowUTC()
	if c.StartAt != nil && now.Before(*c.StartAt) {
		return nil, api.NewError(api.CodeCouponNotStarted)
	}
	if c.ExpireAt != nil && now.After(*c.ExpireAt) {
		return nil, api.NewError(api.CodeCouponExpired)
	}
	if !c.HasQuota() {
		return nil, api.NewError(api.CodeCouponUsedUp)
	}

	// 适用范围
	if c.Scope == model.CouponScopeProducts {
		ok, err := s.repo.AppliesToProduct(ctx, tx, c.ID, productID)
		if err != nil {
			return nil, api.WrapError(api.CodeInternal, err)
		}
		if !ok {
			return nil, api.NewError(api.CodeCouponNotApplicable)
		}
	}

	// 最低消费门槛
	if c.MinAmount > 0 && originalAmount < c.MinAmount {
		return nil, api.NewErrorf(api.CodeCouponMinAmount,
			"该优惠券需满 %s 可用", utils.FormatAmount(c.MinAmount))
	}

	// 个人限次
	if c.PerUserLimit > 0 {
		email = utils.NormalizeEmail(email)
		if email == "" {
			return nil, api.NewErrorf(api.CodeCouponUserLimit, "使用该优惠券需要先填写邮箱")
		}
		used, err := s.repo.CountUsageByEmail(ctx, tx, c.ID, email)
		if err != nil {
			return nil, api.WrapError(api.CodeInternal, err)
		}
		if used >= c.PerUserLimit {
			return nil, api.NewError(api.CodeCouponUserLimit)
		}
	}

	discount := c.CalcDiscount(originalAmount)
	// 抵扣后为 0 的券无法走支付流程（网关不受理 0 元交易），
	// 在校验阶段就拒掉，避免用户填完信息才在下单时失败。
	if originalAmount-discount <= 0 {
		return nil, api.NewErrorf(api.CodeCouponNotApplicable,
			"该优惠券会把应付金额抵扣为 0，无法用于支付")
	}
	return &DiscountResult{
		Coupon:         c,
		CouponID:       c.ID,
		CouponCode:     c.Code,
		CouponName:     c.Name,
		OriginalAmount: originalAmount,
		DiscountAmount: discount,
		PayAmount:      originalAmount - discount,
	}, nil
}

// Redeem 在支付成功事务内核销优惠券。**必须在事务中调用。**
//
// 幂等保证（两道）：
//  1. 先查 coupon_usages 是否已有 (coupon_id, order_id) 记录 → 重复回调直接跳过
//  2. 插入时若撞唯一约束（并发场景）→ 同样视为已核销
//
// 返回 false 表示"额度已被抢光"。此时**不返回错误**：钱已经收到了，
// 绝不能因为优惠券额度问题让已支付订单失败。调用方会记录 needs_attention。
func (s *CouponService) Redeem(ctx context.Context, tx *gorm.DB, order *model.Order) (bool, error) {
	if order.CouponID == 0 || order.DiscountAmount <= 0 {
		return true, nil
	}

	exists, err := s.repo.UsageExists(ctx, tx, order.CouponID, order.ID)
	if err != nil {
		return false, err
	}
	if exists {
		return true, nil // 已核销过（重复回调），幂等返回成功
	}

	ok, err := s.repo.TryConsume(ctx, tx, order.CouponID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil // 额度耗尽
	}

	usage := &model.CouponUsage{
		CouponID:       order.CouponID,
		OrderID:        order.ID,
		OrderNo:        order.OrderNo,
		Email:          order.Email,
		DiscountAmount: order.DiscountAmount,
	}
	if err := s.repo.CreateUsage(ctx, tx, usage); err != nil {
		if database.IsDuplicate(err) {
			// 并发下另一个事务先插入了。回退刚才多加的那一次计数，保持账目正确。
			if rerr := s.repo.ReleaseConsume(ctx, tx, order.CouponID); rerr != nil {
				logger.L().Error("回退优惠券计数失败", "coupon_id", order.CouponID, "err", rerr)
			}
			return true, nil
		}
		return false, err
	}
	return true, nil
}

// ReleaseRedemption 退款时归还优惠券额度。必须在事务中调用。
func (s *CouponService) ReleaseRedemption(ctx context.Context, tx *gorm.DB, order *model.Order) error {
	if order.CouponID == 0 {
		return nil
	}
	exists, err := s.repo.UsageExists(ctx, tx, order.CouponID, order.ID)
	if err != nil || !exists {
		return err
	}
	if err := s.repo.DeleteUsageByOrder(ctx, tx, order.CouponID, order.ID); err != nil {
		return err
	}
	return s.repo.ReleaseConsume(ctx, tx, order.CouponID)
}

// ---- 后台管理 ----

// CouponInput 是创建/更新优惠券的入参。
type CouponInput struct {
	Code         string   `json:"code"`
	Name         string   `json:"name"`
	Type         string   `json:"type" binding:"required,oneof=fixed percent"`
	Value        int64    `json:"value"`
	MinAmount    int64    `json:"min_amount"`
	MaxDiscount  int64    `json:"max_discount"`
	Scope        string   `json:"scope" binding:"required,oneof=all products"`
	ProductIDs   []uint64 `json:"product_ids"`
	UsageLimit   int64    `json:"usage_limit"`
	PerUserLimit int64    `json:"per_user_limit"`
	StartAt      string   `json:"start_at"`
	ExpireAt     string   `json:"expire_at"`
	Status       string   `json:"status"`
}

func (in *CouponInput) normalize() error {
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	if in.Code == "" {
		in.Code = utils.RandomCouponCode(10)
	}
	if len(in.Code) > 64 {
		return api.NewErrorf(api.CodeValidation, "优惠券码过长")
	}
	in.Name = utils.TrimAndLimit(in.Name, 100)
	if in.Name == "" {
		in.Name = in.Code
	}
	if in.Status != model.StatusDisabled {
		in.Status = model.StatusActive
	}

	switch in.Type {
	case model.CouponFixed:
		if in.Value <= 0 {
			return api.NewErrorf(api.CodeValidation, "固定优惠金额必须大于 0")
		}
	case model.CouponPercent:
		// 万分比：9 折 = 9000。允许 1~9999，10000 表示不打折（无意义），0 表示免单
		if in.Value < 0 || in.Value >= model.PercentBase {
			return api.NewErrorf(api.CodeValidation, "折扣值必须在 0-9999 之间（9 折请填 9000）")
		}
	}
	if in.MinAmount < 0 || in.MaxDiscount < 0 || in.UsageLimit < 0 || in.PerUserLimit < 0 {
		return api.NewErrorf(api.CodeValidation, "金额与次数不能为负数")
	}
	if in.Scope == model.CouponScopeProducts && len(in.ProductIDs) == 0 {
		return api.NewErrorf(api.CodeValidation, "选择「指定商品」时必须至少关联一个商品")
	}
	if in.Scope != model.CouponScopeProducts {
		in.ProductIDs = nil
	}
	in.ProductIDs = utils.DedupeUint64(in.ProductIDs)
	return nil
}

func (in *CouponInput) applyTo(c *model.Coupon) error {
	start, err := parseTimeInput(in.StartAt)
	if err != nil {
		return api.NewErrorf(api.CodeValidation, "开始时间格式不正确")
	}
	expire, err := parseTimeInput(in.ExpireAt)
	if err != nil {
		return api.NewErrorf(api.CodeValidation, "过期时间格式不正确")
	}
	if start != nil && expire != nil && !expire.After(*start) {
		return api.NewErrorf(api.CodeValidation, "过期时间必须晚于开始时间")
	}

	c.Code = in.Code
	c.Name = in.Name
	c.Type = in.Type
	c.Value = in.Value
	c.MinAmount = in.MinAmount
	c.MaxDiscount = in.MaxDiscount
	c.Scope = in.Scope
	c.UsageLimit = in.UsageLimit
	c.PerUserLimit = in.PerUserLimit
	c.StartAt = start
	c.ExpireAt = expire
	c.Status = in.Status
	return nil
}

// Create 创建优惠券。
func (s *CouponService) Create(ctx context.Context, in *CouponInput) (*model.Coupon, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}

	var out model.Coupon
	err := s.db.Tx(ctx, func(tx *gorm.DB) error {
		dup, err := s.repo.CodeExists(ctx, tx, in.Code, 0)
		if err != nil {
			return err
		}
		if dup {
			return api.NewError(api.CodeCouponCodeDup)
		}
		if err := s.checkProducts(ctx, tx, in.ProductIDs); err != nil {
			return err
		}
		if err := in.applyTo(&out); err != nil {
			return err
		}
		if err := s.repo.Create(ctx, tx, &out); err != nil {
			return err
		}
		return s.repo.ReplaceProducts(ctx, tx, out.ID, in.ProductIDs)
	})
	if err != nil {
		return nil, wrapServiceErr(err)
	}
	out.ProductIDs = in.ProductIDs
	return &out, nil
}

// Update 更新优惠券。
func (s *CouponService) Update(ctx context.Context, id uint64, in *CouponInput) (*model.Coupon, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}

	var out model.Coupon
	err := s.db.Tx(ctx, func(tx *gorm.DB) error {
		existing, err := s.repo.FindByID(ctx, tx, id)
		if err != nil {
			if database.IsNotFound(err) {
				return api.NewError(api.CodeCouponInvalid)
			}
			return err
		}
		dup, err := s.repo.CodeExists(ctx, tx, in.Code, id)
		if err != nil {
			return err
		}
		if dup {
			return api.NewError(api.CodeCouponCodeDup)
		}
		if err := s.checkProducts(ctx, tx, in.ProductIDs); err != nil {
			return err
		}

		out = *existing
		if err := in.applyTo(&out); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, tx, &out); err != nil {
			return err
		}
		return s.repo.ReplaceProducts(ctx, tx, id, in.ProductIDs)
	})
	if err != nil {
		return nil, wrapServiceErr(err)
	}
	out.ProductIDs = in.ProductIDs
	return &out, nil
}

// checkProducts 确认关联的商品都存在。
func (s *CouponService) checkProducts(ctx context.Context, tx *gorm.DB, ids []uint64) error {
	if len(ids) == 0 {
		return nil
	}
	found, err := s.product.FindByIDs(ctx, tx, ids)
	if err != nil {
		return err
	}
	if len(found) != len(ids) {
		return api.NewErrorf(api.CodeProductNotFound, "关联的商品中有不存在的项")
	}
	return nil
}

// Delete 删除优惠券（保留历史核销记录）。
func (s *CouponService) Delete(ctx context.Context, id uint64) error {
	return wrapServiceErr(s.db.Tx(ctx, func(tx *gorm.DB) error {
		if _, err := s.repo.FindByID(ctx, tx, id); err != nil {
			if database.IsNotFound(err) {
				return api.NewError(api.CodeCouponInvalid)
			}
			return err
		}
		return s.repo.Delete(ctx, tx, id)
	}))
}

// Get 查询优惠券详情（含关联商品）。
func (s *CouponService) Get(ctx context.Context, id uint64) (*model.Coupon, error) {
	c, err := s.repo.FindByID(ctx, nil, id)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, api.NewError(api.CodeCouponInvalid)
		}
		return nil, api.WrapError(api.CodeInternal, err)
	}
	ids, err := s.repo.ProductIDs(ctx, nil, id)
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	c.ProductIDs = ids
	if len(ids) > 0 {
		if products, err := s.product.FindByIDs(ctx, nil, ids); err == nil {
			c.Products = products
		}
	}
	return c, nil
}

// List 分页查询优惠券。
func (s *CouponService) List(ctx context.Context, q repository.CouponQuery) ([]model.Coupon, int64, error) {
	list, total, err := s.repo.List(ctx, nil, q)
	if err != nil {
		return nil, 0, api.WrapError(api.CodeInternal, err)
	}
	// 补上关联商品 ID，方便后台列表直接展示
	for i := range list {
		if list[i].Scope == model.CouponScopeProducts {
			ids, _ := s.repo.ProductIDs(ctx, nil, list[i].ID)
			list[i].ProductIDs = ids
		}
	}
	return list, total, nil
}

// ListUsages 查询核销记录。
func (s *CouponService) ListUsages(ctx context.Context, couponID uint64, offset, limit int) ([]model.CouponUsage, int64, error) {
	list, total, err := s.repo.ListUsages(ctx, nil, couponID, offset, limit)
	if err != nil {
		return nil, 0, api.WrapError(api.CodeInternal, err)
	}
	// 邮箱脱敏：后台列表没必要展示完整买家邮箱
	for i := range list {
		list[i].Email = utils.MaskEmail(list[i].Email)
	}
	return list, total, nil
}
