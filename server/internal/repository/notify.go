package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/utils"
)

// NotifyRepo 处理商家通知日志。
type NotifyRepo struct{ Base }

// Create 写入一条通知记录。
func (r *NotifyRepo) Create(ctx context.Context, tx *gorm.DB, l *model.NotifyLog) error {
	if l.CreatedAt.IsZero() {
		l.CreatedAt = utils.NowUTC()
	}
	l.Error = utils.TrimAndLimit(l.Error, 1000)
	l.Title = utils.TrimAndLimit(l.Title, 250)
	return r.conn(ctx, tx).Create(l).Error
}

// NotifyLogQuery 是通知日志查询条件。
type NotifyLogQuery struct {
	Channel string
	Event   string
	Status  string
	Offset  int
	Limit   int
}

// List 分页查询通知日志。
func (r *NotifyRepo) List(ctx context.Context, tx *gorm.DB, q NotifyLogQuery) ([]model.NotifyLog, int64, error) {
	db := r.conn(ctx, tx).Model(&model.NotifyLog{})
	if q.Channel != "" {
		db = db.Where("channel = ?", q.Channel)
	}
	if q.Event != "" {
		db = db.Where("event = ?", q.Event)
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []model.NotifyLog
	err := db.Order("id DESC").Offset(q.Offset).Limit(q.Limit).Find(&out).Error
	return out, total, err
}

// Purge 清理过期通知日志。
func (r *NotifyRepo) Purge(ctx context.Context, tx *gorm.DB, before time.Time) (int64, error) {
	res := r.conn(ctx, tx).Where("created_at < ?", before).Delete(&model.NotifyLog{})
	return res.RowsAffected, res.Error
}
