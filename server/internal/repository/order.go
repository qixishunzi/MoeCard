package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/utils"
)

// OrderRepo 是订单仓储。
type OrderRepo struct{ Base }

func (r *OrderRepo) Create(ctx context.Context, tx *gorm.DB, o *model.Order) error {
	now := utils.NowUTC()
	o.CreatedAt, o.UpdatedAt = now, now
	return r.conn(ctx, tx).Create(o).Error
}

func (r *OrderRepo) CreateItems(ctx context.Context, tx *gorm.DB, items []model.OrderItem) error {
	if len(items) == 0 {
		return nil
	}
	now := utils.NowUTC()
	for i := range items {
		items[i].CreatedAt = now
	}
	return r.conn(ctx, tx).Create(&items).Error
}

func (r *OrderRepo) FindByID(ctx context.Context, tx *gorm.DB, id uint64) (*model.Order, error) {
	var o model.Order
	if err := r.conn(ctx, tx).First(&o, id).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *OrderRepo) FindByNo(ctx context.Context, tx *gorm.DB, orderNo string) (*model.Order, error) {
	var o model.Order
	if err := r.conn(ctx, tx).Where("order_no = ?", orderNo).First(&o).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *OrderRepo) FindByToken(ctx context.Context, tx *gorm.DB, token string) (*model.Order, error) {
	var o model.Order
	if err := r.conn(ctx, tx).Where("query_token = ?", token).First(&o).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

// FindByNoForUpdate 加行锁读取订单。**支付回调处理的第一步。**
//
// MySQL：SELECT ... FOR UPDATE 真正锁住该行，其他事务必须排队；
// SQLite：Tx 已通过全局写互斥把写事务串行化，等价于串行执行。
//
// 拿到锁后再判断订单状态，才能保证"检查-then-修改"是原子的 ——
// 否则两个并发回调可能同时读到 pending，同时判定"未处理"，同时发货。
func (r *OrderRepo) FindByNoForUpdate(ctx context.Context, tx *gorm.DB, orderNo string) (*model.Order, error) {
	var o model.Order
	db := r.DB().LockForUpdate(r.conn(ctx, tx))
	if err := db.Where("order_no = ?", orderNo).First(&o).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

// FindByIDForUpdate 按 ID 加锁读取订单。
func (r *OrderRepo) FindByIDForUpdate(ctx context.Context, tx *gorm.DB, id uint64) (*model.Order, error) {
	var o model.Order
	db := r.DB().LockForUpdate(r.conn(ctx, tx))
	if err := db.Where("id = ?", id).First(&o).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

// UpdateFields 局部更新订单字段。
func (r *OrderRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uint64, fields map[string]any) error {
	fields["updated_at"] = utils.NowUTC()
	return r.conn(ctx, tx).Model(&model.Order{}).Where("id = ?", id).Updates(fields).Error
}

// UpdateStatusCAS 带前置状态校验的状态更新。
//
// WHERE 中带上 expectFrom，使"读到的状态"与"更新时的状态"必须一致；
// RowsAffected = 0 说明状态已被其他并发流程改变，调用方应放弃本次操作。
func (r *OrderRepo) UpdateStatusCAS(ctx context.Context, tx *gorm.DB, id uint64, expectFrom, to string, extra map[string]any) (bool, error) {
	fields := map[string]any{"status": to, "updated_at": utils.NowUTC()}
	for k, v := range extra {
		fields[k] = v
	}
	res := r.conn(ctx, tx).Model(&model.Order{}).
		Where("id = ? AND status = ?", id, expectFrom).
		Updates(fields)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// FindItems 查询订单明细。
func (r *OrderRepo) FindItems(ctx context.Context, tx *gorm.DB, orderID uint64) ([]model.OrderItem, error) {
	var out []model.OrderItem
	err := r.conn(ctx, tx).Where("order_id = ?", orderID).Order("id ASC").Find(&out).Error
	return out, err
}

// FindItemsBatch 批量查询多个订单的明细（列表页避免 N+1）。
func (r *OrderRepo) FindItemsBatch(ctx context.Context, tx *gorm.DB, orderIDs []uint64) (map[uint64][]model.OrderItem, error) {
	out := make(map[uint64][]model.OrderItem, len(orderIDs))
	if len(orderIDs) == 0 {
		return out, nil
	}
	var items []model.OrderItem
	if err := r.conn(ctx, tx).Where("order_id IN ?", orderIDs).Order("id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	for _, it := range items {
		out[it.OrderID] = append(out[it.OrderID], it)
	}
	return out, nil
}

// UpdateItemDelivery 写入订单明细的发货内容。
func (r *OrderRepo) UpdateItemDelivery(ctx context.Context, tx *gorm.DB, itemID uint64, content string) error {
	return r.conn(ctx, tx).Model(&model.OrderItem{}).
		Where("id = ?", itemID).
		Update("delivery_content", content).Error
}

// OrderQuery 是订单列表查询条件。
type OrderQuery struct {
	Keyword        string // 订单号 / 支付流水号模糊匹配
	OrderNo        string
	Email          string
	ProductID      uint64
	ProductKeyword string
	Status         string
	Provider       string
	ChannelID      uint64
	NeedsAttention *bool
	StartAt        *time.Time
	EndAt          *time.Time
	Offset         int
	Limit          int
}

func (r *OrderRepo) buildQuery(ctx context.Context, tx *gorm.DB, q OrderQuery) *gorm.DB {
	db := r.conn(ctx, tx).Model(&model.Order{})
	if q.OrderNo != "" {
		db = db.Where("order_no = ?", q.OrderNo)
	}
	if q.Keyword != "" {
		kw := "%" + escapeLike(q.Keyword) + "%"
		db = db.Where("order_no LIKE ? OR payment_trade_no LIKE ?", kw, kw)
	}
	if q.Email != "" {
		db = db.Where("email = ?", utils.NormalizeEmail(q.Email))
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.Provider != "" {
		db = db.Where("payment_provider = ?", q.Provider)
	}
	if q.ChannelID > 0 {
		db = db.Where("payment_channel_id = ?", q.ChannelID)
	}
	if q.NeedsAttention != nil {
		db = db.Where("needs_attention = ?", *q.NeedsAttention)
	}
	if q.StartAt != nil {
		db = db.Where("created_at >= ?", *q.StartAt)
	}
	if q.EndAt != nil {
		db = db.Where("created_at < ?", *q.EndAt)
	}
	if q.ProductID > 0 || q.ProductKeyword != "" {
		sub := r.conn(ctx, tx).Model(&model.OrderItem{}).Select("order_id")
		if q.ProductID > 0 {
			sub = sub.Where("product_id = ?", q.ProductID)
		}
		if q.ProductKeyword != "" {
			sub = sub.Where("product_name LIKE ?", "%"+escapeLike(q.ProductKeyword)+"%")
		}
		db = db.Where("id IN (?)", sub)
	}
	return db
}

// List 分页查询订单。
func (r *OrderRepo) List(ctx context.Context, tx *gorm.DB, q OrderQuery) ([]model.Order, int64, error) {
	db := r.buildQuery(ctx, tx, q)
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []model.Order
	err := db.Order("id DESC").Offset(q.Offset).Limit(q.Limit).Find(&out).Error
	return out, total, err
}

// FindExpiring 查询已超时但仍处于待支付状态的订单。
func (r *OrderRepo) FindExpiring(ctx context.Context, tx *gorm.DB, now time.Time, limit int) ([]model.Order, error) {
	var out []model.Order
	err := r.conn(ctx, tx).
		Where("status IN ? AND expired_at IS NOT NULL AND expired_at < ?",
			[]string{model.OrderPending, model.OrderPaying}, now).
		Order("id ASC").Limit(limit).Find(&out).Error
	return out, err
}

// CountByStatus 按状态统计订单数。
func (r *OrderRepo) CountByStatus(ctx context.Context, tx *gorm.DB, statuses []string) (map[string]int64, error) {
	type row struct {
		Status string
		Cnt    int64
	}
	db := r.conn(ctx, tx).Model(&model.Order{}).Select("status, COUNT(*) AS cnt")
	if len(statuses) > 0 {
		db = db.Where("status IN ?", statuses)
	}
	var rows []row
	if err := db.Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, x := range rows {
		out[x.Status] = x.Cnt
	}
	return out, nil
}

// PaidStatuses 是"已收到钱"的订单状态集合，用于营业额统计。
func PaidStatuses() []string {
	return []string{model.OrderPaid, model.OrderWaitingDelivery, model.OrderCompleted}
}

// SumRevenue 统计时间区间内的成交额（分）与订单数。
func (r *OrderRepo) SumRevenue(ctx context.Context, tx *gorm.DB, start, end *time.Time) (int64, int64, error) {
	type result struct {
		Total int64
		Cnt   int64
	}
	db := r.conn(ctx, tx).Model(&model.Order{}).
		Select("COALESCE(SUM(pay_amount), 0) AS total, COUNT(*) AS cnt").
		Where("status IN ?", PaidStatuses())
	if start != nil {
		db = db.Where("paid_at >= ?", *start)
	}
	if end != nil {
		db = db.Where("paid_at < ?", *end)
	}
	var res result
	if err := db.Scan(&res).Error; err != nil {
		return 0, 0, err
	}
	return res.Total, res.Cnt, nil
}

// CountCreated 统计时间区间内创建的订单数。
func (r *OrderRepo) CountCreated(ctx context.Context, tx *gorm.DB, start, end *time.Time) (int64, error) {
	db := r.conn(ctx, tx).Model(&model.Order{})
	if start != nil {
		db = db.Where("created_at >= ?", *start)
	}
	if end != nil {
		db = db.Where("created_at < ?", *end)
	}
	var n int64
	err := db.Count(&n).Error
	return n, err
}

// TrendPoint 是销售趋势的一个数据点。
type TrendPoint struct {
	Date    string `json:"date"`
	Orders  int64  `json:"orders"`
	Revenue int64  `json:"revenue"`
}

// RevenueByDay 逐日统计成交额。
//
// 不使用 DATE_FORMAT / strftime 这类方言函数分组 —— 那会强制业务层区分数据库类型。
// 改为一次性把区间内的已支付订单捞出来，在 Go 里按商城时区聚合：
// 既跨库一致，也顺带解决了"按 UTC 分组会切错日期"的时区问题。
func (r *OrderRepo) RevenueByDay(ctx context.Context, tx *gorm.DB, start, end time.Time, tz string) ([]TrendPoint, error) {
	type row struct {
		PaidAt    time.Time
		PayAmount int64
	}
	var rows []row
	err := r.conn(ctx, tx).Model(&model.Order{}).
		Select("paid_at, pay_amount").
		Where("status IN ? AND paid_at >= ? AND paid_at < ?", PaidStatuses(), start, end).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	loc := utils.LoadLocation(tz)
	agg := make(map[string]*TrendPoint)
	for _, x := range rows {
		key := x.PaidAt.In(loc).Format("2006-01-02")
		p, ok := agg[key]
		if !ok {
			p = &TrendPoint{Date: key}
			agg[key] = p
		}
		p.Orders++
		p.Revenue += x.PayAmount
	}

	// 补齐没有订单的日期，让前端图表连续
	var out []TrendPoint
	for d := start.In(loc); d.Before(end); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		if p, ok := agg[key]; ok {
			out = append(out, *p)
		} else {
			out = append(out, TrendPoint{Date: key})
		}
	}
	return out, nil
}

// Recent 返回最近 n 条订单（Dashboard 用）。
func (r *OrderRepo) Recent(ctx context.Context, tx *gorm.DB, n int) ([]model.Order, error) {
	var out []model.Order
	err := r.conn(ctx, tx).Order("id DESC").Limit(n).Find(&out).Error
	return out, err
}

// CountNeedsAttention 统计需要人工处理的异常订单数。
func (r *OrderRepo) CountNeedsAttention(ctx context.Context, tx *gorm.DB) (int64, error) {
	var n int64
	err := r.conn(ctx, tx).Model(&model.Order{}).Where("needs_attention = ?", true).Count(&n).Error
	return n, err
}

// ExistsByEmail 判断该邮箱是否有过订单（用于优惠券个人限购的快速判断）。
func (r *OrderRepo) ExistsByEmail(ctx context.Context, tx *gorm.DB, email string) (bool, error) {
	var n int64
	err := r.conn(ctx, tx).Model(&model.Order{}).
		Where("email = ?", utils.NormalizeEmail(email)).Limit(1).Count(&n).Error
	return n > 0, err
}
