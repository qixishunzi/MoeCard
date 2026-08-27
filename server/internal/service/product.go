package service

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/database"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/utils"
)

// CategoryService 处理商品分类。
type CategoryService struct {
	db       *database.DB
	repo     *repository.CategoryRepo
	products *repository.ProductRepo
}

// NewCategoryService 构造。
func NewCategoryService(db *database.DB, repo *repository.CategoryRepo, products *repository.ProductRepo) *CategoryService {
	return &CategoryService{db: db, repo: repo, products: products}
}

// CategoryInput 是分类的入参。
type CategoryInput struct {
	Name        string `json:"name" binding:"required"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Sort        int    `json:"sort"`
	Status      string `json:"status"`

	// slugAuto 记录别名是不是系统按拼音生成的。不导出，避免被请求体覆盖。
	slugAuto bool
}

// uniqueSlug 让系统自动生成的别名在冲突时自增后缀。
//
// 别名默认取商品/分类名的汉语拼音，于是两个同名商品必然撞在一起。
// 系统生成的别名遇到冲突就自己让路（vip → vip-2 → vip-3）；
// 管理员手填的别名冲突则一定要报错 ——
// 他以为链接是 /p/vip，结果悄悄变成 /p/vip-2，比直接报错糟糕得多。
//
// exists 由调用方提供，因为商品和分类查的是不同的表，
// 而且创建/更新时要不要排除自身也不一样。
func uniqueSlug(base string, auto bool, exists func(string) (bool, error)) (string, bool, error) {
	dup, err := exists(base)
	if err != nil || !dup {
		return base, false, err
	}
	if !auto {
		return base, true, nil // 交给调用方报"别名已存在"
	}
	for i := 2; i <= 50; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		dup, err := exists(cand)
		if err != nil {
			return "", false, err
		}
		if !dup {
			return cand, false, nil
		}
	}
	// 同名商品超过 50 个属于异常情况，退回随机后缀保证还能建出来
	cand := base + "-" + utils.RandomHex(3)
	dup, err = exists(cand)
	if err != nil {
		return "", false, err
	}
	return cand, dup, nil
}

func (in *CategoryInput) normalize() error {
	in.Name = utils.TrimAndLimit(in.Name, 60)
	if in.Name == "" {
		return api.NewErrorf(api.CodeValidation, "分类名称不能为空")
	}
	in.Slug = strings.ToLower(strings.TrimSpace(in.Slug))
	if in.Slug == "" {
		in.Slug = utils.Slugify(in.Name)
		in.slugAuto = true
	}
	if err := utils.ValidateSlug(in.Slug); err != nil {
		return api.NewErrorf(api.CodeValidation, "%s", err.Error())
	}
	in.Description = utils.TrimAndLimit(in.Description, 480)
	in.Icon = utils.TrimAndLimit(in.Icon, 250)
	// 只认这两个值。写错了当场报错 —— SetProductStatus 那条路径就是这么做的，
	// 这里悄悄改成默认值会让"我明明停用了"变成一个查不出原因的问题。
	switch in.Status {
	case "":
		in.Status = model.StatusActive
	case model.StatusActive, model.StatusDisabled:
	default:
		return api.NewErrorf(api.CodeValidation, "分类状态只能是 active 或 disabled")
	}
	return nil
}

// List 查询分类（activeOnly 用于前台）。
func (s *CategoryService) List(ctx context.Context, activeOnly bool) ([]model.Category, error) {
	list, err := s.repo.List(ctx, nil, activeOnly)
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	counts, err := s.repo.CountProductsBatch(ctx, nil, activeOnly)
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	for i := range list {
		list[i].ProductCount = counts[list[i].ID]
	}
	return list, nil
}

// Get 查询单个分类。
func (s *CategoryService) Get(ctx context.Context, id uint64) (*model.Category, error) {
	c, err := s.repo.FindByID(ctx, nil, id)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, api.NewError(api.CodeCategoryNotFound)
		}
		return nil, api.WrapError(api.CodeInternal, err)
	}
	return c, nil
}

// Create 创建分类。
func (s *CategoryService) Create(ctx context.Context, in *CategoryInput) (*model.Category, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	slug, dup, err := uniqueSlug(in.Slug, in.slugAuto, func(v string) (bool, error) {
		return s.repo.SlugExists(ctx, nil, v, 0)
	})
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	if dup {
		return nil, api.NewError(api.CodeCategorySlugDup)
	}
	in.Slug = slug
	c := &model.Category{
		Name: in.Name, Slug: in.Slug, Description: in.Description,
		Icon: in.Icon, Sort: in.Sort, Status: in.Status,
	}
	if err := s.repo.Create(ctx, nil, c); err != nil {
		return nil, wrapServiceErr(err)
	}
	return c, nil
}

// Update 更新分类。
func (s *CategoryService) Update(ctx context.Context, id uint64, in *CategoryInput) (*model.Category, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	c, err := s.repo.FindByID(ctx, nil, id)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, api.NewError(api.CodeCategoryNotFound)
		}
		return nil, api.WrapError(api.CodeInternal, err)
	}
	slug, dup, err := uniqueSlug(in.Slug, in.slugAuto, func(v string) (bool, error) {
		return s.repo.SlugExists(ctx, nil, v, id)
	})
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	if dup {
		return nil, api.NewError(api.CodeCategorySlugDup)
	}
	in.Slug = slug

	c.Name, c.Slug, c.Description = in.Name, in.Slug, in.Description
	c.Icon, c.Sort, c.Status = in.Icon, in.Sort, in.Status
	if err := s.repo.Update(ctx, nil, c); err != nil {
		return nil, wrapServiceErr(err)
	}
	return c, nil
}

// Delete 删除分类。
//
// 删除保护：分类下还有商品时拒绝删除，必须先转移或删除商品。
// 否则商品会变成"孤儿"，前台分类筛选和后台管理都会出现异常数据。
func (s *CategoryService) Delete(ctx context.Context, id uint64) error {
	return wrapServiceErr(s.db.Tx(ctx, func(tx *gorm.DB) error {
		if _, err := s.repo.FindByID(ctx, tx, id); err != nil {
			if database.IsNotFound(err) {
				return api.NewError(api.CodeCategoryNotFound)
			}
			return err
		}
		n, err := s.repo.CountProducts(ctx, tx, id)
		if err != nil {
			return err
		}
		if n > 0 {
			return api.NewErrorf(api.CodeCategoryHasItems,
				"该分类下还有 %d 个商品，请先转移或删除这些商品", n)
		}
		return s.repo.Delete(ctx, tx, id)
	}))
}

// MoveProducts 把商品从一个分类批量转移到另一个分类。
func (s *CategoryService) MoveProducts(ctx context.Context, from, to uint64) (int64, error) {
	var moved int64
	err := s.db.Tx(ctx, func(tx *gorm.DB) error {
		if _, err := s.repo.FindByID(ctx, tx, to); err != nil {
			if database.IsNotFound(err) {
				return api.NewErrorf(api.CodeCategoryNotFound, "目标分类不存在")
			}
			return err
		}
		var err error
		moved, err = s.products.MoveCategory(ctx, tx, from, to)
		return err
	})
	return moved, wrapServiceErr(err)
}

// ---------------------------------------------------------------------------

// ProductService 处理商品。
type ProductService struct {
	db         *database.DB
	repo       *repository.ProductRepo
	codes      *repository.CodeRepo
	categories *repository.CategoryRepo
	settings   *SettingService
	notifier   *NotifyService
}

// NewProductService 构造。
func NewProductService(db *database.DB, repos *repository.Repositories, settings *SettingService, notifier *NotifyService) *ProductService {
	return &ProductService{
		db: db, repo: repos.Product, codes: repos.Code, categories: repos.Category,
		settings: settings, notifier: notifier,
	}
}

// ProductInput 是商品的入参。
type ProductInput struct {
	CategoryID    uint64 `json:"category_id" binding:"required"`
	Name          string `json:"name" binding:"required"`
	Slug          string `json:"slug"`
	Cover         string `json:"cover"`
	Summary       string `json:"summary"`
	Description   string `json:"description"`
	Price         int64  `json:"price"`
	OriginalPrice int64  `json:"original_price"`
	// Stock 用指针：区分"没传这个字段"与"显式传了 0"。
	// 用值类型时，调用方只想改个商品名、请求里没带 stock，
	// Go 的零值会把库存直接清成 0 —— 商品瞬间变成已售罄。
	Stock        *int64 `json:"stock"`
	DeliveryType string `json:"delivery_type" binding:"required,oneof=auto manual"`
	Status       string `json:"status"`
	Sort         int    `json:"sort"`
	IsRecommend  bool   `json:"is_recommend"`
	MinQuantity  int    `json:"min_quantity"`
	MaxQuantity  int    `json:"max_quantity"`
	// LowStockThreshold 为 0 时沿用全局设置。
	LowStockThreshold int                 `json:"low_stock_threshold"`
	CustomFields      []model.CustomField `json:"custom_fields"`

	// slugAuto 记录别名是不是系统按拼音生成的。不导出，避免被请求体覆盖。
	slugAuto bool
}

// StockOrDefault 返回提交的库存值；未提交时返回 fallback（更新时即"保持原值"）。
func (in *ProductInput) StockOrDefault(fallback int64) int64 {
	if in.Stock == nil {
		return fallback
	}
	return *in.Stock
}

func (in *ProductInput) normalize() error {
	in.Name = utils.TrimAndLimit(in.Name, 180)
	if in.Name == "" {
		return api.NewErrorf(api.CodeValidation, "商品名称不能为空")
	}
	in.Slug = strings.ToLower(strings.TrimSpace(in.Slug))
	if in.Slug == "" {
		in.Slug = utils.Slugify(in.Name)
		in.slugAuto = true
	}
	if err := utils.ValidateSlug(in.Slug); err != nil {
		return api.NewErrorf(api.CodeValidation, "%s", err.Error())
	}
	if in.Price <= 0 {
		return api.NewErrorf(api.CodeValidation, "商品价格必须大于 0")
	}
	if in.OriginalPrice < 0 {
		in.OriginalPrice = 0
	}
	if in.MinQuantity < 1 {
		in.MinQuantity = 1
	}
	if in.MaxQuantity < in.MinQuantity {
		in.MaxQuantity = 100
	}
	if in.MaxQuantity > 10000 {
		in.MaxQuantity = 10000
	}
	// 同上：不认识的状态值直接拒，不猜。默认（不传）时保持下架，
	// 让"没说清楚"的商品不会自己出现在前台。
	switch in.Status {
	case "":
		in.Status = model.ProductStatusOff
	case model.ProductStatusOn, model.ProductStatusOff:
	default:
		return api.NewErrorf(api.CodeValidation, "商品状态只能是 on 或 off")
	}
	// 自动发货商品的库存来自卡密数量，stock 字段固定为 0，避免两处库存打架
	if in.DeliveryType == model.DeliveryAuto {
		zero := int64(0)
		in.Stock = &zero
	} else if in.Stock != nil && *in.Stock < model.StockUnlimited {
		// 小于 -1 的值没有意义（-1 已表示无限库存），归零处理
		zero := int64(0)
		in.Stock = &zero
	}

	if in.LowStockThreshold < 0 {
		in.LowStockThreshold = 0
	}
	fields, err := ValidateCustomFields(in.CustomFields)
	if err != nil {
		return err
	}
	in.CustomFields = fields

	in.Cover = utils.TrimAndLimit(in.Cover, 480)
	in.Summary = utils.TrimAndLimit(in.Summary, 480)
	// 富文本必须净化：管理员账号一旦被盗，商品描述就是最直接的 XSS 投放点
	in.Description = utils.SanitizeHTML(in.Description)
	return nil
}

// ProductListOptions 是商品列表查询选项。
type ProductListOptions struct {
	repository.ProductQuery
	WithStock bool // 是否计算实时可用库存
	// Public 表示这是前台请求。目前只影响销量要不要下发，
	// 后台的列表任何时候都该看得见真实销量。
	Public bool
}

// List 分页查询商品。
func (s *ProductService) List(ctx context.Context, opt ProductListOptions) ([]model.Product, int64, error) {
	list, total, err := s.repo.List(ctx, nil, opt.ProductQuery)
	if err != nil {
		return nil, 0, api.WrapError(api.CodeInternal, err)
	}
	if len(list) == 0 {
		return list, total, nil
	}

	// 批量补齐分类名与可用库存，避免 N+1 查询
	cats, err := s.categories.List(ctx, nil, false)
	if err == nil {
		nameOf := make(map[uint64]string, len(cats))
		for _, c := range cats {
			nameOf[c.ID] = c.Name
		}
		for i := range list {
			list[i].CategoryName = nameOf[list[i].CategoryID]
		}
	}

	if opt.WithStock {
		if err := s.fillStock(ctx, list); err != nil {
			return nil, 0, err
		}
	}
	if opt.Public {
		s.hideSalesIfConfigured(list)
	}
	return list, total, nil
}

// hideSalesIfConfigured 在店主关掉"显示已售数量"时，把销量从响应里抹掉。
//
// 只在前台路径上调用 —— 不能塞进 fillStock，那条路后台也在走，
// 后台的商品列表本来就该看得见真实销量。
//
// 抹的是接口返回值而不是只让前端不渲染：藏在页面上但接口照发，
// 等于没藏 —— 打开开发者工具就能看见。
func (s *ProductService) hideSalesIfConfigured(list []model.Product) {
	if s.settings.ShowSales() {
		return
	}
	for i := range list {
		list[i].SalesCount = 0
	}
}

// fillStock 计算每个商品的实时可用库存。
//
//	auto   → 未使用卡密数量
//	manual → stock 字段（-1 表示无限）
func (s *ProductService) fillStock(ctx context.Context, list []model.Product) error {
	var autoIDs []uint64
	for _, p := range list {
		if p.IsAuto() {
			autoIDs = append(autoIDs, p.ID)
		}
	}
	counts := map[uint64]int64{}
	if len(autoIDs) > 0 {
		var err error
		counts, err = s.codes.CountAvailableBatch(ctx, nil, autoIDs)
		if err != nil {
			return api.WrapError(api.CodeInternal, err)
		}
	}
	for i := range list {
		if list[i].IsAuto() {
			list[i].AvailableStock = counts[list[i].ID]
		} else {
			list[i].AvailableStock = list[i].Stock
		}
		// 所有商品读取路径都汇聚到这里，自定义字段在这一处解析即可全覆盖
		list[i].CustomFieldList = ParseCustomFields(list[i].CustomFields)
	}
	return nil
}

// GetBySlug 前台商品详情。
func (s *ProductService) GetBySlug(ctx context.Context, slug string) (*model.Product, error) {
	p, err := s.repo.FindBySlug(ctx, nil, slug)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, api.NewError(api.CodeProductNotFound)
		}
		return nil, api.WrapError(api.CodeInternal, err)
	}
	if !p.IsOnSale() {
		return nil, api.NewError(api.CodeProductOffShelf)
	}
	one := []model.Product{*p}
	if err := s.fillStock(ctx, one); err != nil {
		return nil, err
	}
	if c, err := s.categories.FindByID(ctx, nil, p.CategoryID); err == nil {
		one[0].CategoryName = c.Name
	}
	s.hideSalesIfConfigured(one)
	return &one[0], nil
}

// GetByID 后台商品详情。
func (s *ProductService) GetByID(ctx context.Context, id uint64) (*model.Product, error) {
	p, err := s.repo.FindByID(ctx, nil, id)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, api.NewError(api.CodeProductNotFound)
		}
		return nil, api.WrapError(api.CodeInternal, err)
	}
	one := []model.Product{*p}
	if err := s.fillStock(ctx, one); err != nil {
		return nil, err
	}
	return &one[0], nil
}

// Create 创建商品。
func (s *ProductService) Create(ctx context.Context, in *ProductInput) (*model.Product, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	customFieldsJSON, err := EncodeCustomFields(in.CustomFields)
	if err != nil {
		return nil, err
	}
	var out model.Product
	err = s.db.Tx(ctx, func(tx *gorm.DB) error {
		if _, err := s.categories.FindByID(ctx, tx, in.CategoryID); err != nil {
			if database.IsNotFound(err) {
				return api.NewError(api.CodeCategoryNotFound)
			}
			return err
		}
		slug, dup, err := uniqueSlug(in.Slug, in.slugAuto, func(v string) (bool, error) {
			return s.repo.SlugExists(ctx, tx, v, 0)
		})
		if err != nil {
			return err
		}
		if dup {
			return api.NewError(api.CodeProductSlugDup)
		}
		in.Slug = slug
		out = model.Product{
			CategoryID: in.CategoryID, Name: in.Name, Slug: in.Slug,
			Cover: in.Cover, Summary: in.Summary, Description: in.Description,
			Price: in.Price, OriginalPrice: in.OriginalPrice, Stock: in.StockOrDefault(0),
			DeliveryType: in.DeliveryType, Status: in.Status, Sort: in.Sort,
			IsRecommend: in.IsRecommend, MinQuantity: in.MinQuantity, MaxQuantity: in.MaxQuantity,
			LowStockThreshold: in.LowStockThreshold, CustomFields: customFieldsJSON,
		}
		return s.repo.Create(ctx, tx, &out)
	})
	if err != nil {
		return nil, wrapServiceErr(err)
	}
	return &out, nil
}

// Update 更新商品。
func (s *ProductService) Update(ctx context.Context, id uint64, in *ProductInput) (*model.Product, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	customFieldsJSON, err := EncodeCustomFields(in.CustomFields)
	if err != nil {
		return nil, err
	}
	var out model.Product
	err = s.db.Tx(ctx, func(tx *gorm.DB) error {
		existing, err := s.repo.FindByID(ctx, tx, id)
		if err != nil {
			if database.IsNotFound(err) {
				return api.NewError(api.CodeProductNotFound)
			}
			return err
		}
		if _, err := s.categories.FindByID(ctx, tx, in.CategoryID); err != nil {
			if database.IsNotFound(err) {
				return api.NewError(api.CodeCategoryNotFound)
			}
			return err
		}
		slug, dup, err := uniqueSlug(in.Slug, in.slugAuto, func(v string) (bool, error) {
			return s.repo.SlugExists(ctx, tx, v, id)
		})
		if err != nil {
			return err
		}
		if dup {
			return api.NewError(api.CodeProductSlugDup)
		}
		in.Slug = slug

		// 已有卡密的商品不允许从 auto 改成 manual：
		// 那些卡密会变成没人管的孤儿数据，且已售订单的发货来源会对不上。
		if existing.DeliveryType == model.DeliveryAuto && in.DeliveryType == model.DeliveryManual {
			stats, err := s.codes.StatsByProduct(ctx, tx, id)
			if err != nil {
				return err
			}
			if stats[model.CodeStatusUnused]+stats[model.CodeStatusLocked]+stats[model.CodeStatusSold] > 0 {
				return api.NewErrorf(api.CodeValidation,
					"该商品已存在卡密记录，不能改为手动发货。如需切换请先清空未使用卡密并新建商品")
			}
		}

		out = *existing
		out.CategoryID, out.Name, out.Slug = in.CategoryID, in.Name, in.Slug
		out.Cover, out.Summary, out.Description = in.Cover, in.Summary, in.Description
		// 未提交 stock 时保留原库存，不要清零
		out.Price, out.OriginalPrice = in.Price, in.OriginalPrice
		out.Stock = in.StockOrDefault(existing.Stock)
		out.DeliveryType, out.Status, out.Sort = in.DeliveryType, in.Status, in.Sort
		out.IsRecommend, out.MinQuantity, out.MaxQuantity = in.IsRecommend, in.MinQuantity, in.MaxQuantity
		out.LowStockThreshold = in.LowStockThreshold
		out.CustomFields = customFieldsJSON
		// 库存回到阈值以上时清掉告警标记，否则补货后再次跌破不会提醒
		if out.LowStockNotifiedAt != nil && !out.IsAuto() && out.Stock > int64(in.LowStockThreshold) {
			out.LowStockNotifiedAt = nil
		}
		return s.repo.Update(ctx, tx, &out)
	})
	if err != nil {
		return nil, wrapServiceErr(err)
	}
	return &out, nil
}

// SetStatus 上下架商品。
func (s *ProductService) SetStatus(ctx context.Context, id uint64, status string) error {
	if status != model.ProductStatusOn && status != model.ProductStatusOff {
		return api.NewErrorf(api.CodeValidation, "状态只能是 on 或 off")
	}
	return wrapServiceErr(s.repo.UpdateFields(ctx, nil, id, map[string]any{"status": status}))
}

// UpdateStock 修改手动发货商品的库存。
func (s *ProductService) UpdateStock(ctx context.Context, id uint64, stock int64) error {
	p, err := s.repo.FindByID(ctx, nil, id)
	if err != nil {
		if database.IsNotFound(err) {
			return api.NewError(api.CodeProductNotFound)
		}
		return api.WrapError(api.CodeInternal, err)
	}
	if p.IsAuto() {
		return api.NewErrorf(api.CodeValidation,
			"自动发货商品的库存由卡密数量决定，请在「卡密管理」中导入或删除卡密")
	}
	if stock < model.StockUnlimited {
		return api.NewErrorf(api.CodeValidation, "库存不能小于 -1（-1 表示无限库存）")
	}
	return wrapServiceErr(s.repo.UpdateFields(ctx, nil, id, map[string]any{"stock": stock}))
}

// Delete 软删商品。
//
// 商品永远只做软删：历史订单虽然有快照，但后台仍需要能追溯到原商品记录。
func (s *ProductService) Delete(ctx context.Context, id uint64) error {
	return wrapServiceErr(s.db.Tx(ctx, func(tx *gorm.DB) error {
		if _, err := s.repo.FindByID(ctx, tx, id); err != nil {
			if database.IsNotFound(err) {
				return api.NewError(api.CodeProductNotFound)
			}
			return err
		}
		// 未使用的卡密一并清掉，避免删了商品还占着库存统计
		if _, err := s.codes.DeleteUnused(ctx, tx, id, nil); err != nil {
			return err
		}
		return s.repo.SoftDelete(ctx, tx, id)
	}))
}
