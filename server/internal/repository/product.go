package repository

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/utils"
)

// escapeLike 剔除 LIKE 通配符，防止用户输入 "%" 导致全表扫描或匹配失控。
//
// 这里不采用反斜杠转义：SQLite 的 LIKE 默认**没有**转义字符（必须显式写 ESCAPE 子句），
// 而 MySQL 默认就是反斜杠 —— 同一份 SQL 会在两种数据库上产生不同结果。
// 搜索框场景下直接剔除通配符是最简单且跨库行为一致的做法。
func escapeLike(s string) string {
	return strings.NewReplacer("%", "", "_", "", `\`, "").Replace(s)
}

// CategoryRepo 是分类仓储。
type CategoryRepo struct{ Base }

func (r *CategoryRepo) FindByID(ctx context.Context, tx *gorm.DB, id uint64) (*model.Category, error) {
	var c model.Category
	if err := r.conn(ctx, tx).First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *CategoryRepo) FindBySlug(ctx context.Context, tx *gorm.DB, slug string) (*model.Category, error) {
	var c model.Category
	if err := r.conn(ctx, tx).Where("slug = ?", slug).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// List 查询分类。activeOnly=true 时只返回启用的分类（前台用）。
func (r *CategoryRepo) List(ctx context.Context, tx *gorm.DB, activeOnly bool) ([]model.Category, error) {
	db := r.conn(ctx, tx).Model(&model.Category{})
	if activeOnly {
		db = db.Where("status = ?", model.StatusActive)
	}
	var out []model.Category
	err := db.Order("sort DESC, id ASC").Find(&out).Error
	return out, err
}

func (r *CategoryRepo) Create(ctx context.Context, tx *gorm.DB, c *model.Category) error {
	now := utils.NowUTC()
	c.CreatedAt, c.UpdatedAt = now, now
	return r.conn(ctx, tx).Create(c).Error
}

func (r *CategoryRepo) Update(ctx context.Context, tx *gorm.DB, c *model.Category) error {
	c.UpdatedAt = utils.NowUTC()
	return r.conn(ctx, tx).Model(&model.Category{}).Where("id = ?", c.ID).
		Select("name", "slug", "description", "icon", "sort", "status", "updated_at").
		Updates(c).Error
}

func (r *CategoryRepo) Delete(ctx context.Context, tx *gorm.DB, id uint64) error {
	return r.conn(ctx, tx).Delete(&model.Category{}, id).Error
}

// SlugExists 判断 slug 是否被占用（excludeID 用于更新时排除自身）。
func (r *CategoryRepo) SlugExists(ctx context.Context, tx *gorm.DB, slug string, excludeID uint64) (bool, error) {
	db := r.conn(ctx, tx).Model(&model.Category{}).Where("slug = ?", slug)
	if excludeID > 0 {
		db = db.Where("id <> ?", excludeID)
	}
	var n int64
	err := db.Count(&n).Error
	return n > 0, err
}

// CountProducts 统计分类下未软删的商品数（删除保护用）。
func (r *CategoryRepo) CountProducts(ctx context.Context, tx *gorm.DB, categoryID uint64) (int64, error) {
	var n int64
	err := r.conn(ctx, tx).Model(&model.Product{}).
		Where("category_id = ? AND deleted_at IS NULL", categoryID).Count(&n).Error
	return n, err
}

// CountProductsBatch 批量统计各分类商品数。
func (r *CategoryRepo) CountProductsBatch(ctx context.Context, tx *gorm.DB, onSaleOnly bool) (map[uint64]int64, error) {
	type row struct {
		CategoryID uint64
		Cnt        int64
	}
	db := r.conn(ctx, tx).Model(&model.Product{}).
		Select("category_id, COUNT(*) AS cnt").
		Where("deleted_at IS NULL")
	if onSaleOnly {
		db = db.Where("status = ?", model.ProductStatusOn)
	}
	var rows []row
	if err := db.Group("category_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[uint64]int64, len(rows))
	for _, x := range rows {
		out[x.CategoryID] = x.Cnt
	}
	return out, nil
}

// ---------------------------------------------------------------------------

// ProductRepo 是商品仓储。
type ProductRepo struct{ Base }

// productSortWhitelist 是排序字段白名单。
//
// ORDER BY 的列名会被拼进 SQL，绝不能直接使用用户输入 —— 这是 SQL 注入的经典入口。
var productSortWhitelist = map[string]string{
	"default":    "sort DESC, id DESC",
	"newest":     "id DESC",
	"price_asc":  "price ASC, id DESC",
	"price_desc": "price DESC, id DESC",
	"sales":      "sales_count DESC, id DESC",
}

// ProductQuery 是商品列表查询条件。
type ProductQuery struct {
	CategoryID     uint64
	Keyword        string
	Status         string
	DeliveryType   string
	Recommend      *bool
	Sort           string
	Offset         int
	Limit          int
	IncludeDeleted bool
}

// List 分页查询商品。
func (r *ProductRepo) List(ctx context.Context, tx *gorm.DB, q ProductQuery) ([]model.Product, int64, error) {
	db := r.conn(ctx, tx).Model(&model.Product{})
	if !q.IncludeDeleted {
		db = db.Where("deleted_at IS NULL")
	}
	if q.CategoryID > 0 {
		db = db.Where("category_id = ?", q.CategoryID)
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.DeliveryType != "" {
		db = db.Where("delivery_type = ?", q.DeliveryType)
	}
	if q.Recommend != nil {
		db = db.Where("is_recommend = ?", *q.Recommend)
	}
	if q.Keyword != "" {
		kw := "%" + escapeLike(q.Keyword) + "%"
		db = db.Where("name LIKE ? OR summary LIKE ? OR slug LIKE ?", kw, kw, kw)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	order := utils.SortWhitelist(q.Sort, productSortWhitelist, productSortWhitelist["default"])
	var out []model.Product
	err := db.Order(order).Offset(q.Offset).Limit(q.Limit).Find(&out).Error
	return out, total, err
}

func (r *ProductRepo) FindByID(ctx context.Context, tx *gorm.DB, id uint64) (*model.Product, error) {
	var p model.Product
	if err := r.conn(ctx, tx).Where("id = ? AND deleted_at IS NULL", id).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// FindByIDIncludeDeleted 用于后台查看已删除商品的历史信息。
func (r *ProductRepo) FindByIDIncludeDeleted(ctx context.Context, tx *gorm.DB, id uint64) (*model.Product, error) {
	var p model.Product
	if err := r.conn(ctx, tx).Where("id = ?", id).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *ProductRepo) FindBySlug(ctx context.Context, tx *gorm.DB, slug string) (*model.Product, error) {
	var p model.Product
	if err := r.conn(ctx, tx).Where("slug = ? AND deleted_at IS NULL", slug).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// FindForUpdate 加行锁读取商品（MySQL 生效；SQLite 由全局写锁保证）。
func (r *ProductRepo) FindForUpdate(ctx context.Context, tx *gorm.DB, id uint64) (*model.Product, error) {
	var p model.Product
	db := r.DB().LockForUpdate(r.conn(ctx, tx))
	if err := db.Where("id = ? AND deleted_at IS NULL", id).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *ProductRepo) FindByIDs(ctx context.Context, tx *gorm.DB, ids []uint64) ([]model.Product, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var out []model.Product
	err := r.conn(ctx, tx).Where("id IN ?", ids).Find(&out).Error
	return out, err
}

func (r *ProductRepo) Create(ctx context.Context, tx *gorm.DB, p *model.Product) error {
	now := utils.NowUTC()
	p.CreatedAt, p.UpdatedAt = now, now
	return r.conn(ctx, tx).Create(p).Error
}

// Update 全量更新商品的可编辑字段。
//
// 这里用 Select 白名单而不是 Save：Save 会把 sales_count、created_at、deleted_at
// 一并写回，任何一个陈旧的内存副本都能把销量清零。
//
// **白名单是有代价的：新加一个可编辑的列，必须同时加到这里。**
// 漏掉的那一列不会报错 —— 接口照样返回"保存成功"，只是那个值永远写不进去。
// custom_fields 和 low_stock_threshold 就这样漏过一次。
func (r *ProductRepo) Update(ctx context.Context, tx *gorm.DB, p *model.Product) error {
	p.UpdatedAt = utils.NowUTC()
	return r.conn(ctx, tx).Model(&model.Product{}).Where("id = ?", p.ID).
		Select("category_id", "name", "slug", "cover", "summary", "description",
			"price", "original_price", "stock", "delivery_type", "status",
			"sort", "is_recommend", "min_quantity", "max_quantity",
			"low_stock_threshold", "low_stock_notified_at", "custom_fields",
			"updated_at").
		Updates(p).Error
}

// UpdateFields 局部更新指定字段。
func (r *ProductRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uint64, fields map[string]any) error {
	fields["updated_at"] = utils.NowUTC()
	return r.conn(ctx, tx).Model(&model.Product{}).Where("id = ?", id).Updates(fields).Error
}

// SoftDelete 软删商品。历史订单已有快照，因此软删不影响订单展示。
func (r *ProductRepo) SoftDelete(ctx context.Context, tx *gorm.DB, id uint64) error {
	now := utils.NowUTC()
	return r.conn(ctx, tx).Model(&model.Product{}).Where("id = ?", id).
		Updates(map[string]any{"deleted_at": now, "status": model.ProductStatusOff, "updated_at": now}).Error
}

func (r *ProductRepo) SlugExists(ctx context.Context, tx *gorm.DB, slug string, excludeID uint64) (bool, error) {
	db := r.conn(ctx, tx).Model(&model.Product{}).Where("slug = ?", slug)
	if excludeID > 0 {
		db = db.Where("id <> ?", excludeID)
	}
	var n int64
	err := db.Count(&n).Error
	return n > 0, err
}

// DeductStock 扣减手动发货商品的库存（CAS，禁止超卖）。
//
// 返回 false 表示库存不足，调用方必须回滚事务。
// stock = -1（无限库存）直接放行且不做任何扣减。
func (r *ProductRepo) DeductStock(ctx context.Context, tx *gorm.DB, productID uint64, qty int64) (bool, error) {
	if qty <= 0 {
		return false, nil
	}
	// CAS：只有当前库存 >= 需求量时才扣减。
	// 两个并发事务不可能都通过这个条件 —— 数据库的行锁保证了这一点。
	res := r.conn(ctx, tx).Model(&model.Product{}).
		Where("id = ? AND stock >= ? AND stock <> ?", productID, qty, model.StockUnlimited).
		UpdateColumn("stock", gorm.Expr("stock - ?", qty))
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected > 0 {
		return true, nil
	}

	// 没扣成：要么是无限库存（放行），要么是真的不够（拒绝）。
	var p model.Product
	if err := r.conn(ctx, tx).Select("id", "stock", "delivery_type").
		Where("id = ?", productID).First(&p).Error; err != nil {
		return false, err
	}
	return p.Stock == model.StockUnlimited, nil
}

// RestoreStock 归还手动发货商品的库存（订单过期/取消/退款时）。
func (r *ProductRepo) RestoreStock(ctx context.Context, tx *gorm.DB, productID uint64, qty int64) error {
	if qty <= 0 {
		return nil
	}
	return r.conn(ctx, tx).Model(&model.Product{}).
		Where("id = ? AND stock <> ?", productID, model.StockUnlimited).
		UpdateColumn("stock", gorm.Expr("stock + ?", qty)).Error
}

// IncrSales 增加销量。
func (r *ProductRepo) IncrSales(ctx context.Context, tx *gorm.DB, productID uint64, qty int64) error {
	return r.conn(ctx, tx).Model(&model.Product{}).
		Where("id = ?", productID).
		UpdateColumn("sales_count", gorm.Expr("sales_count + ?", qty)).Error
}

// Count 统计商品数量。
func (r *ProductRepo) Count(ctx context.Context, tx *gorm.DB, onSaleOnly bool) (int64, error) {
	db := r.conn(ctx, tx).Model(&model.Product{}).Where("deleted_at IS NULL")
	if onSaleOnly {
		db = db.Where("status = ?", model.ProductStatusOn)
	}
	var n int64
	err := db.Count(&n).Error
	return n, err
}

// HasOrders 判断商品是否有历史订单（决定能否物理删除）。
func (r *ProductRepo) HasOrders(ctx context.Context, tx *gorm.DB, productID uint64) (bool, error) {
	var n int64
	err := r.conn(ctx, tx).Model(&model.OrderItem{}).
		Where("product_id = ?", productID).Limit(1).Count(&n).Error
	return n > 0, err
}

// MoveCategory 把某分类下的商品批量转移到另一分类。
func (r *ProductRepo) MoveCategory(ctx context.Context, tx *gorm.DB, from, to uint64) (int64, error) {
	res := r.conn(ctx, tx).Model(&model.Product{}).
		Where("category_id = ? AND deleted_at IS NULL", from).
		Updates(map[string]any{"category_id": to, "updated_at": utils.NowUTC()})
	return res.RowsAffected, res.Error
}
