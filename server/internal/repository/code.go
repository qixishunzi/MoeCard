package repository

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/utils"
)

// CodeRepo 是卡密仓储。
//
// 这是整个系统并发安全性要求最高的地方 —— 绝不能把同一个卡密发给两个订单。
type CodeRepo struct{ Base }

// CountAvailable 统计某商品可用（未使用）卡密数量。
func (r *CodeRepo) CountAvailable(ctx context.Context, tx *gorm.DB, productID uint64) (int64, error) {
	var n int64
	err := r.conn(ctx, tx).Model(&model.ProductCode{}).
		Where("product_id = ? AND status = ?", productID, model.CodeStatusUnused).
		Count(&n).Error
	return n, err
}

// CountAvailableBatch 批量统计多个商品的可用卡密数量（列表页避免 N+1 查询）。
func (r *CodeRepo) CountAvailableBatch(ctx context.Context, tx *gorm.DB, productIDs []uint64) (map[uint64]int64, error) {
	out := make(map[uint64]int64, len(productIDs))
	if len(productIDs) == 0 {
		return out, nil
	}
	type row struct {
		ProductID uint64
		Cnt       int64
	}
	var rows []row
	err := r.conn(ctx, tx).Model(&model.ProductCode{}).
		Select("product_id, COUNT(*) AS cnt").
		Where("product_id IN ? AND status = ?", productIDs, model.CodeStatusUnused).
		Group("product_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, x := range rows {
		out[x.ProductID] = x.Cnt
	}
	return out, nil
}

// Claim 为订单占用指定数量的卡密（unused → locked）。
//
// **并发安全的核心实现。** 必须在事务内调用。
//
// 算法：
//  1. 先 SELECT 出候选 ID（多取一些以容忍竞争）
//  2. 用带条件的 UPDATE 做 CAS：WHERE id IN (...) AND status = 'unused'
//     —— 数据库保证只有一个事务能把某一行从 unused 改走，
//     RowsAffected 告诉我们真正抢到了几张
//  3. 不够就再循环抢，直到够数或候选耗尽
//
// 为什么不用 "UPDATE ... WHERE id IN (SELECT ...)"：
// MySQL 不允许在 UPDATE 中直接子查询同一张表（ERROR 1093），
// 而两步 CAS 在 SQLite 和 MySQL 上语义完全一致，无需分支。
//
// 返回抢到的卡密（长度必然等于 quantity，否则返回错误由调用方回滚）。
func (r *CodeRepo) Claim(ctx context.Context, tx *gorm.DB, productID, orderID, orderItemID uint64, quantity int) ([]model.ProductCode, error) {
	if tx == nil {
		return nil, fmt.Errorf("Claim 必须在事务中调用")
	}
	if quantity <= 0 {
		return nil, fmt.Errorf("invalid quantity %d", quantity)
	}

	db := tx.WithContext(ctx)
	now := utils.NowUTC()
	claimed := make([]uint64, 0, quantity)

	// 最多尝试 5 轮。正常情况下 1 轮即可完成；
	// 只有在高并发抢购同一商品时才会进入第 2 轮。
	for round := 0; round < 5 && len(claimed) < quantity; round++ {
		need := quantity - len(claimed)

		var candidates []uint64
		q := db.Model(&model.ProductCode{}).
			Where("product_id = ? AND status = ?", productID, model.CodeStatusUnused).
			Order("id ASC").
			Limit(need*2). // 多取一倍，减少竞争导致的轮次
			Pluck("id", &candidates)
		if q.Error != nil {
			return nil, fmt.Errorf("select candidate codes: %w", q.Error)
		}
		if len(candidates) == 0 {
			break // 确实没库存了
		}
		if len(candidates) > need {
			candidates = candidates[:need]
		}

		res := db.Model(&model.ProductCode{}).
			Where("id IN ? AND status = ?", candidates, model.CodeStatusUnused).
			Updates(map[string]any{
				"status":        model.CodeStatusLocked,
				"order_id":      orderID,
				"order_item_id": orderItemID,
				"locked_at":     now,
			})
		if res.Error != nil {
			return nil, fmt.Errorf("lock codes: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			continue // 全被别人抢走了，下一轮
		}

		// 只有真正被本次 UPDATE 命中的行才算抢到。
		// 通过 order_id + locked_at 精确回捞，避免把别人锁的行算进来。
		var got []uint64
		if err := db.Model(&model.ProductCode{}).
			Where("id IN ? AND status = ? AND order_id = ?", candidates, model.CodeStatusLocked, orderID).
			Pluck("id", &got).Error; err != nil {
			return nil, fmt.Errorf("confirm locked codes: %w", err)
		}
		for _, id := range got {
			if len(claimed) >= quantity {
				break
			}
			if !containsUint64(claimed, id) {
				claimed = append(claimed, id)
			}
		}
	}

	if len(claimed) < quantity {
		// 抢不够就报错，由 service 回滚整个事务 —— 已锁定的行会随事务回滚自动释放。
		return nil, fmt.Errorf("库存不足: 需要 %d，实际可用 %d", quantity, len(claimed))
	}

	var codes []model.ProductCode
	if err := db.Where("id IN ?", claimed).Order("id ASC").Find(&codes).Error; err != nil {
		return nil, fmt.Errorf("load claimed codes: %w", err)
	}
	// 发货路径上解密失败必须中断：把密文当卡密发给买家比报错严重得多
	if err := decryptCodes(codes, true); err != nil {
		return nil, err
	}
	return codes, nil
}

// decryptCodes 就地把卡密内容还原成明文。
//
// strict=true 用于发货路径：解密失败直接返回错误，宁可这单转人工，
// 也绝不能把 "enc:v1:..." 当成卡密发给买家。
// strict=false 用于后台列表：单条坏数据不应该让整页打不开，
// 用占位提示替代，管理员一眼就能看出是哪条有问题。
func decryptCodes(codes []model.ProductCode, strict bool) error {
	for i := range codes {
		plain, err := utils.Decrypt(codes[i].Content)
		if err != nil {
			if strict {
				return fmt.Errorf("卡密解密失败(id=%d): %w", codes[i].ID, err)
			}
			plain = utils.MustDecrypt(codes[i].Content)
		}
		codes[i].Content = plain
	}
	return nil
}

// MarkSold 把订单已锁定的卡密标记为已售出。必须在事务内调用。
//
// 只处理 status='locked' 的行 —— 已经是 sold 的不会被重复处理，
// 因此重复的支付回调不会造成任何副作用（幂等）。
func (r *CodeRepo) MarkSold(ctx context.Context, tx *gorm.DB, orderID uint64) (int64, error) {
	if tx == nil {
		return 0, fmt.Errorf("MarkSold 必须在事务中调用")
	}
	now := utils.NowUTC()
	res := tx.WithContext(ctx).Model(&model.ProductCode{}).
		Where("order_id = ? AND status = ?", orderID, model.CodeStatusLocked).
		Updates(map[string]any{"status": model.CodeStatusSold, "sold_at": now})
	return res.RowsAffected, res.Error
}

// Release 释放订单锁定的卡密（locked → unused）。
//
// 用于订单过期/取消。已 sold 的卡密永不释放 —— 那是已经交付给用户的东西。
func (r *CodeRepo) Release(ctx context.Context, tx *gorm.DB, orderID uint64) (int64, error) {
	if tx == nil {
		return 0, fmt.Errorf("Release 必须在事务中调用")
	}
	res := tx.WithContext(ctx).Model(&model.ProductCode{}).
		Where("order_id = ? AND status = ?", orderID, model.CodeStatusLocked).
		Updates(map[string]any{
			"status":        model.CodeStatusUnused,
			"order_id":      0,
			"order_item_id": 0,
			"locked_at":     nil,
		})
	return res.RowsAffected, res.Error
}

// FindByOrder 查询订单关联的卡密（locked 或 sold）。
func (r *CodeRepo) FindByOrder(ctx context.Context, tx *gorm.DB, orderID uint64) ([]model.ProductCode, error) {
	var out []model.ProductCode
	if err := r.conn(ctx, tx).
		Where("order_id = ? AND status IN ?", orderID, []string{model.CodeStatusLocked, model.CodeStatusSold}).
		Order("id ASC").Find(&out).Error; err != nil {
		return nil, err
	}
	// 订单详情要展示给买家，解密失败必须报错而不是把密文当卡密显示
	if err := decryptCodes(out, true); err != nil {
		return nil, err
	}
	return out, nil
}

// ImportResult 是批量导入卡密的结果统计。
type ImportResult struct {
	Total     int      `json:"total"`     // 提交的原始行数
	Imported  int      `json:"imported"`  // 实际写入数
	Duplicate int      `json:"duplicate"` // 因重复被跳过的数量
	Samples   []string `json:"samples"`   // 部分重复样例（脱敏），便于管理员核对
}

// Import 批量导入卡密。
//
// 去重分两层：
//  1. 本次提交内部去重（同一批里出现两次）
//  2. 与数据库已有卡密去重（content_hash 唯一索引兜底）
//
// 采用逐条 FirstOrCreate 而非批量 INSERT IGNORE：
// INSERT IGNORE / ON CONFLICT 的语法在两种数据库上不一致，
// 而卡密导入是低频操作，逐条插入的性能完全可接受，换来的是零方言分支。
func (r *CodeRepo) Import(ctx context.Context, tx *gorm.DB, productID uint64, contents []string) (*ImportResult, error) {
	res := &ImportResult{Total: len(contents)}
	if len(contents) == 0 {
		return res, nil
	}

	db := r.conn(ctx, tx)
	now := utils.NowUTC()

	// 1. 批内去重
	unique := utils.DedupeStrings(contents)
	res.Duplicate += len(contents) - len(unique)

	// 2. 与数据库已有的比对
	hashes := make([]string, 0, len(unique))
	hashOf := make(map[string]string, len(unique))
	for _, c := range unique {
		h := utils.CodeContentHash(productID, c)
		hashes = append(hashes, h)
		hashOf[c] = h
	}

	existing := make(map[string]bool)
	// 分批查询，避免 IN 子句参数过多（SQLite 默认上限 999）
	for i := 0; i < len(hashes); i += 500 {
		end := min(i+500, len(hashes))
		var found []string
		if err := db.Model(&model.ProductCode{}).
			Where("product_id = ? AND content_hash IN ?", productID, hashes[i:end]).
			Pluck("content_hash", &found).Error; err != nil {
			return nil, fmt.Errorf("check duplicate codes: %w", err)
		}
		for _, h := range found {
			existing[h] = true
		}
	}

	batch := make([]model.ProductCode, 0, len(unique))
	for _, c := range unique {
		h := hashOf[c]
		if existing[h] {
			res.Duplicate++
			if len(res.Samples) < 5 {
				res.Samples = append(res.Samples, utils.MaskCardCode(c))
			}
			continue
		}
		existing[h] = true

		// 静态加密：ContentHash 始终基于明文计算，去重逻辑因此不受加密影响。
		stored, err := utils.Encrypt(c)
		if err != nil {
			return nil, fmt.Errorf("encrypt code: %w", err)
		}
		batch = append(batch, model.ProductCode{
			ProductID:   productID,
			Content:     stored,
			ContentHash: h,
			Encrypted:   utils.IsEncrypted(stored),
			Status:      model.CodeStatusUnused,
			CreatedAt:   now,
		})
	}

	if len(batch) > 0 {
		if err := db.CreateInBatches(&batch, 200).Error; err != nil {
			return nil, fmt.Errorf("insert codes: %w", err)
		}
	}
	res.Imported = len(batch)
	return res, nil
}

// CodeQuery 是卡密列表查询条件。
type CodeQuery struct {
	ProductID uint64
	Status    string
	Keyword   string
	OrderNo   string
	Offset    int
	Limit     int
}

// List 分页查询卡密。
func (r *CodeRepo) List(ctx context.Context, tx *gorm.DB, q CodeQuery) ([]model.ProductCode, int64, error) {
	db := r.conn(ctx, tx).Model(&model.ProductCode{})
	if q.ProductID > 0 {
		db = db.Where("product_id = ?", q.ProductID)
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.Keyword != "" {
		// 启用静态加密后 content 是密文，LIKE 搜不到任何东西。
		// 改为「按内容哈希精确匹配」+「LIKE 兜底」：
		// 前者覆盖"粘贴整条卡密查它卖给了谁"这个真实用途，
		// 后者让加密开启前写入的历史明文仍可模糊搜索。
		like := "%" + escapeLike(q.Keyword) + "%"
		if q.ProductID > 0 {
			db = db.Where("content_hash = ? OR content LIKE ?",
				utils.CodeContentHash(q.ProductID, strings.TrimSpace(q.Keyword)), like)
		} else {
			db = db.Where("content LIKE ?", like)
		}
	}
	if q.OrderNo != "" {
		db = db.Where("order_id IN (?)",
			r.conn(ctx, tx).Model(&model.Order{}).Select("id").Where("order_no = ?", q.OrderNo))
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []model.ProductCode
	if err := db.Order("id DESC").Offset(q.Offset).Limit(q.Limit).Find(&out).Error; err != nil {
		return nil, 0, err
	}
	_ = decryptCodes(out, false)
	return out, total, nil
}

// DeleteUnused 删除未使用的卡密。
//
// 只允许删除 unused —— locked/sold 的卡密关联着真实订单，
// 物理删除会导致用户在订单详情里看不到已购买的内容。
func (r *CodeRepo) DeleteUnused(ctx context.Context, tx *gorm.DB, productID uint64, ids []uint64) (int64, error) {
	db := r.conn(ctx, tx).Where("product_id = ? AND status = ?", productID, model.CodeStatusUnused)
	if len(ids) > 0 {
		db = db.Where("id IN ?", ids)
	}
	res := db.Delete(&model.ProductCode{})
	return res.RowsAffected, res.Error
}

// DeleteUnusedByIDs 跨商品批量删除未使用卡密。
//
// 卡密总览页里勾选的行可能来自不同商品，按商品逐个调用 DeleteUnused
// 会变成 N 次事务；这里一条语句解决，语义仍然是"只删 unused"。
func (r *CodeRepo) DeleteUnusedByIDs(ctx context.Context, tx *gorm.DB, ids []uint64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res := r.conn(ctx, tx).
		Where("id IN ? AND status = ?", ids, model.CodeStatusUnused).
		Delete(&model.ProductCode{})
	return res.RowsAffected, res.Error
}

// StatsGlobal 返回全站卡密各状态数量。
func (r *CodeRepo) StatsGlobal(ctx context.Context, tx *gorm.DB) (map[string]int64, error) {
	type row struct {
		Status string
		Cnt    int64
	}
	var rows []row
	err := r.conn(ctx, tx).Model(&model.ProductCode{}).
		Select("status, COUNT(*) AS cnt").Group("status").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := map[string]int64{
		model.CodeStatusUnused: 0,
		model.CodeStatusLocked: 0,
		model.CodeStatusSold:   0,
	}
	for _, x := range rows {
		out[x.Status] = x.Cnt
	}
	return out, nil
}

// ProductStock 是单个商品的卡密库存明细。
type ProductStock struct {
	ProductID   uint64 `json:"product_id"`
	ProductName string `json:"product_name"`
	Unused      int64  `json:"unused"`
	Locked      int64  `json:"locked"`
	Sold        int64  `json:"sold"`
}

// StockByProduct 一次查出每个商品的卡密库存分布。
//
// 两步而不是一条 SQL：
//   - 先列出全部在售的自动发货商品，哪怕它一张卡都没有 ——
//     "一张卡都不剩"恰恰是最该被看见的状态，而按卡密表分组永远查不出它；
//   - 再叠加卡密表里的实际计数，顺带把商品已删除、卡密还留着的孤儿数据带出来。
//
// 计数用 SUM(CASE WHEN ...) 而不是三条 COUNT：
// CASE WHEN 在 SQLite 和 MySQL 上写法完全一致，不需要方言分支。
func (r *CodeRepo) StockByProduct(ctx context.Context, tx *gorm.DB) ([]ProductStock, error) {
	var products []model.Product
	if err := r.conn(ctx, tx).Model(&model.Product{}).
		Select("id", "name").
		Where("deleted_at IS NULL AND delivery_type = ?", model.DeliveryAuto).
		Find(&products).Error; err != nil {
		return nil, err
	}

	out := make([]ProductStock, 0, len(products))
	index := make(map[uint64]int, len(products))
	for _, p := range products {
		index[p.ID] = len(out)
		out = append(out, ProductStock{ProductID: p.ID, ProductName: p.Name})
	}

	var counts []ProductStock
	if err := r.conn(ctx, tx).Model(&model.ProductCode{}).
		Select(`product_codes.product_id AS product_id,
			products.name AS product_name,
			SUM(CASE WHEN product_codes.status = ? THEN 1 ELSE 0 END) AS unused,
			SUM(CASE WHEN product_codes.status = ? THEN 1 ELSE 0 END) AS locked,
			SUM(CASE WHEN product_codes.status = ? THEN 1 ELSE 0 END) AS sold`,
			model.CodeStatusUnused, model.CodeStatusLocked, model.CodeStatusSold).
		Joins("LEFT JOIN products ON products.id = product_codes.product_id").
		Group("product_codes.product_id, products.name").
		Scan(&counts).Error; err != nil {
		return nil, err
	}
	for _, c := range counts {
		if i, ok := index[c.ProductID]; ok {
			out[i].Unused, out[i].Locked, out[i].Sold = c.Unused, c.Locked, c.Sold
			continue
		}
		out = append(out, c) // 商品已删除或已改成手动发货，但卡密还在
	}

	// 可用卡密少的排前面；同样少时按商品 ID 稳定排序，避免每次刷新顺序都在跳
	sort.Slice(out, func(i, j int) bool {
		if out[i].Unused != out[j].Unused {
			return out[i].Unused < out[j].Unused
		}
		return out[i].ProductID < out[j].ProductID
	})
	return out, nil
}

// DeleteByID 删除单条未使用卡密。
func (r *CodeRepo) DeleteByID(ctx context.Context, tx *gorm.DB, id uint64) (int64, error) {
	res := r.conn(ctx, tx).
		Where("id = ? AND status = ?", id, model.CodeStatusUnused).
		Delete(&model.ProductCode{})
	return res.RowsAffected, res.Error
}

// StatsByProduct 返回某商品各状态卡密数量。
func (r *CodeRepo) StatsByProduct(ctx context.Context, tx *gorm.DB, productID uint64) (map[string]int64, error) {
	type row struct {
		Status string
		Cnt    int64
	}
	var rows []row
	err := r.conn(ctx, tx).Model(&model.ProductCode{}).
		Select("status, COUNT(*) AS cnt").
		Where("product_id = ?", productID).
		Group("status").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := map[string]int64{
		model.CodeStatusUnused: 0,
		model.CodeStatusLocked: 0,
		model.CodeStatusSold:   0,
	}
	for _, x := range rows {
		out[x.Status] = x.Cnt
	}
	return out, nil
}

// TotalUnused 统计全站未使用卡密总数（Dashboard 用）。
func (r *CodeRepo) TotalUnused(ctx context.Context, tx *gorm.DB) (int64, error) {
	var n int64
	err := r.conn(ctx, tx).Model(&model.ProductCode{}).
		Where("status = ?", model.CodeStatusUnused).Count(&n).Error
	return n, err
}

// ReleaseStaleLocks 释放长时间处于 locked 且其订单已终结的卡密。
//
// 这是一道保险：正常流程下过期任务会释放库存，
// 但如果进程在事务提交后、后续步骤前崩溃，可能留下孤儿锁定。
func (r *CodeRepo) ReleaseStaleLocks(ctx context.Context, tx *gorm.DB, before time.Time) (int64, error) {
	sub := r.conn(ctx, tx).Model(&model.Order{}).
		Select("id").
		Where("status IN ?", []string{model.OrderCancelled, model.OrderExpired})

	res := r.conn(ctx, tx).Model(&model.ProductCode{}).
		Where("status = ? AND locked_at < ? AND order_id IN (?)", model.CodeStatusLocked, before, sub).
		Updates(map[string]any{
			"status":        model.CodeStatusUnused,
			"order_id":      0,
			"order_item_id": 0,
			"locked_at":     nil,
		})
	return res.RowsAffected, res.Error
}

func containsUint64(s []uint64, v uint64) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
