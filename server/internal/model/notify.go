package model

import "time"

// 通知发送状态。
const (
	NotifyStatusSuccess = "success"
	NotifyStatusFailed  = "failed"
)

// NotifyLog 记录每一次商家通知的发送结果。
//
// 与 EmailLog 同样的定位：通知失败绝不能影响主业务（订单该发货照样发货），
// 因此这里是唯一的失败留痕 —— 没有它，通知悄悄失效了也没人知道。
type NotifyLog struct {
	ID        uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Channel   string    `gorm:"column:channel;size:24" json:"channel"`
	Event     string    `gorm:"column:event;size:32" json:"event"`
	Title     string    `gorm:"column:title;size:255" json:"title"`
	Content   string    `gorm:"column:content" json:"content"`
	Status    string    `gorm:"column:status;size:16" json:"status"`
	Error     string    `gorm:"column:error;size:1000" json:"error"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
}

func (NotifyLog) TableName() string { return "notify_logs" }
