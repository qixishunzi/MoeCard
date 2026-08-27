package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/utils"
)

// ---------------------------------------------------------------------------
// AdminRepo
// ---------------------------------------------------------------------------

// AdminRepo 是管理员仓储。
type AdminRepo struct{ Base }

func (r *AdminRepo) FindByID(ctx context.Context, tx *gorm.DB, id uint64) (*model.Admin, error) {
	var a model.Admin
	if err := r.conn(ctx, tx).First(&a, id).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *AdminRepo) FindByUsername(ctx context.Context, tx *gorm.DB, username string) (*model.Admin, error) {
	var a model.Admin
	if err := r.conn(ctx, tx).Where("username = ?", username).First(&a).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *AdminRepo) Create(ctx context.Context, tx *gorm.DB, a *model.Admin) error {
	now := utils.NowUTC()
	a.CreatedAt, a.UpdatedAt = now, now
	if a.TokenVersion == 0 {
		a.TokenVersion = 1
	}
	return r.conn(ctx, tx).Create(a).Error
}

func (r *AdminRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uint64, fields map[string]any) error {
	fields["updated_at"] = utils.NowUTC()
	return r.conn(ctx, tx).Model(&model.Admin{}).Where("id = ?", id).Updates(fields).Error
}

func (r *AdminRepo) Delete(ctx context.Context, tx *gorm.DB, id uint64) error {
	return r.conn(ctx, tx).Delete(&model.Admin{}, id).Error
}

func (r *AdminRepo) List(ctx context.Context, tx *gorm.DB, offset, limit int) ([]model.Admin, int64, error) {
	db := r.conn(ctx, tx).Model(&model.Admin{})
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []model.Admin
	err := db.Order("id ASC").Offset(offset).Limit(limit).Find(&out).Error
	return out, total, err
}

func (r *AdminRepo) UsernameExists(ctx context.Context, tx *gorm.DB, username string, excludeID uint64) (bool, error) {
	db := r.conn(ctx, tx).Model(&model.Admin{}).Where("username = ?", username)
	if excludeID > 0 {
		db = db.Where("id <> ?", excludeID)
	}
	var n int64
	err := db.Count(&n).Error
	return n > 0, err
}

// CountActive 统计可用管理员数量（用于"不能删除最后一个管理员"保护）。
func (r *AdminRepo) CountActive(ctx context.Context, tx *gorm.DB, excludeID uint64) (int64, error) {
	db := r.conn(ctx, tx).Model(&model.Admin{}).Where("status = ?", model.StatusActive)
	if excludeID > 0 {
		db = db.Where("id <> ?", excludeID)
	}
	var n int64
	err := db.Count(&n).Error
	return n, err
}

// Total 返回管理员总数（用于判断系统是否已初始化）。
func (r *AdminRepo) Total(ctx context.Context, tx *gorm.DB) (int64, error) {
	var n int64
	err := r.conn(ctx, tx).Model(&model.Admin{}).Count(&n).Error
	return n, err
}

// ---------------------------------------------------------------------------
// PaymentRepo
// ---------------------------------------------------------------------------

// PaymentRepo 是支付渠道与支付日志仓储。
type PaymentRepo struct{ Base }

func (r *PaymentRepo) FindChannel(ctx context.Context, tx *gorm.DB, id uint64) (*model.PaymentChannel, error) {
	var c model.PaymentChannel
	if err := r.conn(ctx, tx).First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// ListChannels 查询支付渠道。enabledOnly=true 时只返回启用的（前台用）。
func (r *PaymentRepo) ListChannels(ctx context.Context, tx *gorm.DB, enabledOnly bool) ([]model.PaymentChannel, error) {
	db := r.conn(ctx, tx).Model(&model.PaymentChannel{})
	if enabledOnly {
		db = db.Where("status = ?", model.ChannelEnabled)
	}
	var out []model.PaymentChannel
	err := db.Order("sort DESC, id ASC").Find(&out).Error
	return out, err
}

func (r *PaymentRepo) CreateChannel(ctx context.Context, tx *gorm.DB, c *model.PaymentChannel) error {
	now := utils.NowUTC()
	c.CreatedAt, c.UpdatedAt = now, now
	return r.conn(ctx, tx).Create(c).Error
}

func (r *PaymentRepo) UpdateChannel(ctx context.Context, tx *gorm.DB, id uint64, fields map[string]any) error {
	fields["updated_at"] = utils.NowUTC()
	return r.conn(ctx, tx).Model(&model.PaymentChannel{}).Where("id = ?", id).Updates(fields).Error
}

func (r *PaymentRepo) DeleteChannel(ctx context.Context, tx *gorm.DB, id uint64) error {
	return r.conn(ctx, tx).Delete(&model.PaymentChannel{}, id).Error
}

// ChannelHasOrders 判断渠道是否已产生订单（有订单则只允许禁用，不允许删除）。
func (r *PaymentRepo) ChannelHasOrders(ctx context.Context, tx *gorm.DB, channelID uint64) (bool, error) {
	var n int64
	err := r.conn(ctx, tx).Model(&model.Order{}).
		Where("payment_channel_id = ?", channelID).Limit(1).Count(&n).Error
	return n > 0, err
}

// CreateLog 写入支付日志。
//
// 注意：调用方必须已经对 request_data / response_data 做过敏感字段过滤。
func (r *PaymentRepo) CreateLog(ctx context.Context, tx *gorm.DB, l *model.PaymentLog) error {
	l.CreatedAt = utils.NowUTC()
	// 防止超长内容撑爆列（MySQL MEDIUMTEXT 上限 16MB，但没必要存那么多）
	l.RequestData = utils.TrimAndLimit(l.RequestData, 20000)
	l.ResponseData = utils.TrimAndLimit(l.ResponseData, 20000)
	return r.conn(ctx, tx).Create(l).Error
}

// PaymentLogQuery 是支付日志查询条件。
type PaymentLogQuery struct {
	OrderNo  string
	Provider string
	Event    string
	Offset   int
	Limit    int
}

func (r *PaymentRepo) ListLogs(ctx context.Context, tx *gorm.DB, q PaymentLogQuery) ([]model.PaymentLog, int64, error) {
	db := r.conn(ctx, tx).Model(&model.PaymentLog{})
	if q.OrderNo != "" {
		db = db.Where("order_no = ?", q.OrderNo)
	}
	if q.Provider != "" {
		db = db.Where("provider = ?", q.Provider)
	}
	if q.Event != "" {
		db = db.Where("event = ?", q.Event)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []model.PaymentLog
	err := db.Order("id DESC").Offset(q.Offset).Limit(q.Limit).Find(&out).Error
	return out, total, err
}

// ---------------------------------------------------------------------------
// SettingRepo
// ---------------------------------------------------------------------------

// SettingRepo 是系统配置仓储。
type SettingRepo struct{ Base }

// All 返回全部配置。
func (r *SettingRepo) All(ctx context.Context, tx *gorm.DB) (map[string]string, error) {
	var rows []model.SystemSetting
	if err := r.conn(ctx, tx).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, s := range rows {
		out[s.Key] = s.Value
	}
	return out, nil
}

// Get 读取单项配置。
func (r *SettingRepo) Get(ctx context.Context, tx *gorm.DB, key string) (string, error) {
	var s model.SystemSetting
	if err := r.conn(ctx, tx).Where("setting_key = ?", key).First(&s).Error; err != nil {
		return "", err
	}
	return s.Value, nil
}

// Set 写入单项配置（不存在则创建）。
//
// 用"先更新、影响 0 行再插入"而不是 ON CONFLICT / REPLACE INTO ——
// 后两者的语法在 SQLite 与 MySQL 上不一致，会把方言泄漏到业务层。
func (r *SettingRepo) Set(ctx context.Context, tx *gorm.DB, key, value string, isSecret bool) error {
	db := r.conn(ctx, tx)
	now := utils.NowUTC()

	res := db.Model(&model.SystemSetting{}).Where("setting_key = ?", key).
		Updates(map[string]any{"value": value, "is_secret": isSecret, "updated_at": now})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		return nil
	}
	return db.Create(&model.SystemSetting{
		Key: key, Value: value, IsSecret: isSecret, UpdatedAt: now,
	}).Error
}

// SetMany 批量写入配置。必须在事务中调用以保证原子性。
func (r *SettingRepo) SetMany(ctx context.Context, tx *gorm.DB, kv map[string]string) error {
	for k, v := range kv {
		if err := r.Set(ctx, tx, k, v, model.IsSecretSettingKey(k)); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// LogRepo（管理员操作日志 + 邮件日志）
// ---------------------------------------------------------------------------

// LogRepo 是日志仓储。
type LogRepo struct{ Base }

func (r *LogRepo) CreateAdminLog(ctx context.Context, tx *gorm.DB, l *model.AdminOperationLog) error {
	l.CreatedAt = utils.NowUTC()
	l.Detail = utils.TrimAndLimit(l.Detail, 5000)
	return r.conn(ctx, tx).Create(l).Error
}

// AdminLogQuery 是管理员操作日志查询条件。
type AdminLogQuery struct {
	AdminID uint64
	Action  string
	Keyword string
	Offset  int
	Limit   int
}

func (r *LogRepo) ListAdminLogs(ctx context.Context, tx *gorm.DB, q AdminLogQuery) ([]model.AdminOperationLog, int64, error) {
	db := r.conn(ctx, tx).Model(&model.AdminOperationLog{})
	if q.AdminID > 0 {
		db = db.Where("admin_id = ?", q.AdminID)
	}
	if q.Action != "" {
		db = db.Where("action = ?", q.Action)
	}
	if q.Keyword != "" {
		kw := "%" + escapeLike(q.Keyword) + "%"
		db = db.Where("admin_username LIKE ? OR target_id LIKE ? OR detail LIKE ?", kw, kw, kw)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []model.AdminOperationLog
	err := db.Order("id DESC").Offset(q.Offset).Limit(q.Limit).Find(&out).Error
	return out, total, err
}

func (r *LogRepo) CreateEmailLog(ctx context.Context, tx *gorm.DB, l *model.EmailLog) error {
	l.CreatedAt = utils.NowUTC()
	l.Error = utils.TrimAndLimit(l.Error, 1000)
	return r.conn(ctx, tx).Create(l).Error
}

// EmailLogQuery 是邮件日志查询条件。
type EmailLogQuery struct {
	OrderNo string
	Email   string
	Status  string
	Offset  int
	Limit   int
}

func (r *LogRepo) ListEmailLogs(ctx context.Context, tx *gorm.DB, q EmailLogQuery) ([]model.EmailLog, int64, error) {
	db := r.conn(ctx, tx).Model(&model.EmailLog{})
	if q.OrderNo != "" {
		db = db.Where("order_no = ?", q.OrderNo)
	}
	if q.Email != "" {
		db = db.Where("to_email = ?", utils.NormalizeEmail(q.Email))
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []model.EmailLog
	err := db.Order("id DESC").Offset(q.Offset).Limit(q.Limit).Find(&out).Error
	return out, total, err
}

// PurgeOldLogs 清理过期日志，防止日志表无限膨胀。
func (r *LogRepo) PurgeOldLogs(ctx context.Context, tx *gorm.DB, before time.Time) (int64, error) {
	db := r.conn(ctx, tx)
	var total int64

	res := db.Where("created_at < ?", before).Delete(&model.AdminOperationLog{})
	if res.Error != nil {
		return total, res.Error
	}
	total += res.RowsAffected

	res = db.Where("created_at < ?", before).Delete(&model.EmailLog{})
	if res.Error != nil {
		return total, res.Error
	}
	total += res.RowsAffected

	// 支付日志保留更久（用于对账），这里只清理非关键事件
	res = db.Where("created_at < ? AND event IN ?", before,
		[]string{model.PayEventQuery, model.PayEventCreate}).Delete(&model.PaymentLog{})
	if res.Error != nil {
		return total, res.Error
	}
	total += res.RowsAffected
	return total, nil
}

// FirstSeenAt 返回配置表里最早的一条更新时间。
//
// 这就是「系统部署时间」：EnsureDefaults 在首次启动时写入全部默认配置，
// 所以最早的那一行就是第一次跑起来的时刻。用它而不是新加一列，
// 是为了让已经在跑的实例也能立刻用上，不必迁移。
func (r *SettingRepo) FirstSeenAt(ctx context.Context, tx *gorm.DB) (time.Time, error) {
	// 取最早的一行而不是 SELECT MIN(updated_at)：
	// 聚合结果没有模型字段可对应，SQLite 驱动会把它当字符串交回来，
	// 扫进匿名结构体的 time.Time 时会静默留空 —— 一个不报错的空值最难查。
	// 按 updated_at 升序取一行，走的就是普通的模型字段转换路径。
	var row model.SystemSetting
	err := r.conn(ctx, tx).Order("updated_at ASC").Limit(1).Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return time.Time{}, nil // 还没初始化过，交给调用方按"未知"处理
		}
		return time.Time{}, err
	}
	return row.UpdatedAt, nil
}
