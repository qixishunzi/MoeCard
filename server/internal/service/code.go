package service

import (
	"context"

	"gorm.io/gorm"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/database"
	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/utils"
)

// CodeService 处理卡密管理。
type CodeService struct {
	db       *database.DB
	repo     *repository.CodeRepo
	products *repository.ProductRepo
	orders   *repository.OrderRepo
}

// NewCodeService 构造。
func NewCodeService(db *database.DB, repos *repository.Repositories) *CodeService {
	return &CodeService{db: db, repo: repos.Code, products: repos.Product, orders: repos.Order}
}

// maxImportLines 限制单次导入行数，防止一次贴几十万行把事务撑爆。
const maxImportLines = 20000

// Import 批量导入卡密。
//
// 处理流程（对应 §31 要求）：
//   - 按行切分，trim 空格、忽略空行
//   - 批内去重
//   - 与数据库已有卡密去重（(product_id, content_hash) 唯一索引兜底）
func (s *CodeService) Import(ctx context.Context, productID uint64, raw string) (*repository.ImportResult, error) {
	lines := utils.SplitLines(raw)
	if len(lines) == 0 {
		return nil, api.NewErrorf(api.CodeValidation, "没有可导入的卡密")
	}
	if len(lines) > maxImportLines {
		return nil, api.NewErrorf(api.CodeValidation,
			"单次最多导入 %d 条卡密，本次提交了 %d 条，请分批导入", maxImportLines, len(lines))
	}
	for _, l := range lines {
		if len([]rune(l)) > 900 {
			return nil, api.NewErrorf(api.CodeValidation, "存在超长卡密（单条不能超过 900 字符）")
		}
	}

	var result *repository.ImportResult
	err := s.db.Tx(ctx, func(tx *gorm.DB) error {
		p, err := s.products.FindByID(ctx, tx, productID)
		if err != nil {
			if database.IsNotFound(err) {
				return api.NewError(api.CodeProductNotFound)
			}
			return err
		}
		if !p.IsAuto() {
			return api.NewErrorf(api.CodeValidation, "只有自动发货商品才能导入卡密")
		}
		result, err = s.repo.Import(ctx, tx, productID, lines)
		return err
	})
	if err != nil {
		return nil, wrapServiceErr(err)
	}

	logger.Admin().Info("卡密导入完成",
		"product_id", productID, "total", result.Total,
		"imported", result.Imported, "duplicate", result.Duplicate)
	return result, nil
}

// CodeView 是卡密列表的出参（内容脱敏）。
type CodeView struct {
	model.ProductCode
	MaskedContent string `json:"masked_content"`
	// ProductName 只在跨商品的卡密总览里有意义 ——
	// 那里一屏能看到几十个商品的卡密，光有 product_id 根本认不出是哪个。
	ProductName string `json:"product_name,omitempty"`
}

// List 分页查询卡密。
//
// reveal=false 时卡密内容脱敏。后台列表默认脱敏，
// 只有管理员显式点"查看"才返回完整内容 —— 减少肩窥与截图泄露风险。
func (s *CodeService) List(ctx context.Context, q repository.CodeQuery, reveal bool) ([]CodeView, int64, error) {
	list, total, err := s.repo.List(ctx, nil, q)
	if err != nil {
		return nil, 0, api.WrapError(api.CodeInternal, err)
	}

	// 批量补订单号，避免 N+1
	orderIDs := make([]uint64, 0, len(list))
	for _, c := range list {
		if c.OrderID > 0 {
			orderIDs = append(orderIDs, c.OrderID)
		}
	}
	orderNos := map[uint64]string{}
	if len(orderIDs) > 0 {
		var rows []model.Order
		if err := s.db.DB.WithContext(ctx).Model(&model.Order{}).
			Select("id", "order_no").Where("id IN ?", utils.DedupeUint64(orderIDs)).
			Find(&rows).Error; err == nil {
			for _, o := range rows {
				orderNos[o.ID] = o.OrderNo
			}
		}
	}

	// 批量补商品名，同样避免 N+1。
	//
	// 无条件补，包括按商品过滤时：卡密总览里选中某个商品后 q.ProductID 就不是 0 了，
	// 一旦这里跳过，表格的"商品"列会整列退化成 #3 这样的裸 ID。
	// 按商品过滤时这只是一次单 ID 查询，代价可以忽略。
	productNames := map[uint64]string{}
	if len(list) > 0 {
		ids := make([]uint64, 0, len(list))
		for _, c := range list {
			ids = append(ids, c.ProductID)
		}
		// 用 FindByIDs 而不是过滤软删除：商品删了但卡密还在，
		// 这种孤儿库存恰恰是总览页最该让人看见的东西。
		if rows, err := s.products.FindByIDs(ctx, nil, utils.DedupeUint64(ids)); err == nil {
			for _, p := range rows {
				productNames[p.ID] = p.Name
			}
		}
	}

	out := make([]CodeView, 0, len(list))
	for _, c := range list {
		v := CodeView{
			ProductCode:   c,
			MaskedContent: utils.MaskCardCode(c.Content),
			ProductName:   productNames[c.ProductID],
		}
		v.OrderNo = orderNos[c.OrderID]
		if !reveal {
			v.Content = ""
		}
		out = append(out, v)
	}
	return out, total, nil
}

// StatsGlobal 返回全站卡密各状态数量。
func (s *CodeService) StatsGlobal(ctx context.Context) (map[string]int64, error) {
	stats, err := s.repo.StatsGlobal(ctx, nil)
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	stats["total"] = stats[model.CodeStatusUnused] + stats[model.CodeStatusLocked] + stats[model.CodeStatusSold]
	return stats, nil
}

// StockOverview 返回每个商品的卡密库存分布，按可用数量升序 —— 快没货的排在最前面。
func (s *CodeService) StockOverview(ctx context.Context) ([]repository.ProductStock, error) {
	rows, err := s.repo.StockByProduct(ctx, nil)
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	for i := range rows {
		if rows[i].ProductName == "" {
			rows[i].ProductName = "已删除的商品"
		}
	}
	return rows, nil
}

// DeleteByIDs 跨商品批量删除未使用卡密。
func (s *CodeService) DeleteByIDs(ctx context.Context, ids []uint64) (int64, error) {
	ids = utils.DedupeUint64(ids)
	if len(ids) == 0 {
		return 0, api.NewErrorf(api.CodeValidation, "请选择要删除的卡密")
	}
	if len(ids) > 2000 {
		return 0, api.NewErrorf(api.CodeValidation, "单次最多删除 2000 条")
	}
	n, err := s.repo.DeleteUnusedByIDs(ctx, nil, ids)
	if err != nil {
		return 0, api.WrapError(api.CodeInternal, err)
	}
	if n == 0 {
		// 一条都没删掉，只可能是选中的全是 locked/sold
		return 0, api.NewError(api.CodeCodeInUse)
	}
	return n, nil
}

// Stats 返回商品卡密的各状态数量。
func (s *CodeService) Stats(ctx context.Context, productID uint64) (map[string]int64, error) {
	stats, err := s.repo.StatsByProduct(ctx, nil, productID)
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	stats["total"] = stats[model.CodeStatusUnused] + stats[model.CodeStatusLocked] + stats[model.CodeStatusSold]
	return stats, nil
}

// DeleteUnused 批量删除未使用卡密。
//
// 只能删 unused：locked 的卡密属于进行中的订单，sold 的属于已交付订单。
// 物理删除它们会让用户在订单详情里看不到自己买的东西。
func (s *CodeService) DeleteUnused(ctx context.Context, productID uint64, ids []uint64, allUnused bool) (int64, error) {
	if !allUnused && len(ids) == 0 {
		return 0, api.NewErrorf(api.CodeValidation, "请选择要删除的卡密")
	}
	var n int64
	err := s.db.Tx(ctx, func(tx *gorm.DB) error {
		var err error
		if allUnused {
			n, err = s.repo.DeleteUnused(ctx, tx, productID, nil)
		} else {
			n, err = s.repo.DeleteUnused(ctx, tx, productID, ids)
		}
		return err
	})
	if err != nil {
		return 0, wrapServiceErr(err)
	}
	if n == 0 && !allUnused {
		return 0, api.NewError(api.CodeCodeInUse)
	}
	return n, nil
}

// DeleteOne 删除单条未使用卡密。
func (s *CodeService) DeleteOne(ctx context.Context, id uint64) error {
	n, err := s.repo.DeleteByID(ctx, nil, id)
	if err != nil {
		return api.WrapError(api.CodeInternal, err)
	}
	if n == 0 {
		return api.NewError(api.CodeCodeInUse)
	}
	return nil
}

// TotalUnused 返回全站未使用卡密总数。
func (s *CodeService) TotalUnused(ctx context.Context) (int64, error) {
	return s.repo.TotalUnused(ctx, nil)
}
