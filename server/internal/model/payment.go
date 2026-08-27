package model

import "time"

// 支付渠道状态。
const (
	ChannelEnabled  = "enabled"
	ChannelDisabled = "disabled"
)

// PaymentChannel 是一条支付线路。
//
// provider 与 channel 是两个概念：
//   - provider 是代码里的实现（stripe / alipay / yipay_v1 ...）
//   - channel  是运营配置的一条线路（"Stripe US"、"易支付-备用线"）
//
// 同一 provider 可以建多个 channel，各自持有独立配置。
// 回调 URL 携带 channel_id 才能定位到正确的密钥进行验签。
type PaymentChannel struct {
	Model
	Name     string `gorm:"column:name;size:64" json:"name"`
	Provider string `gorm:"column:provider;size:32" json:"provider"`
	Icon     string `gorm:"column:icon;size:255" json:"icon"`
	Config   string `gorm:"column:config" json:"-"` // JSON 字符串，出参必须脱敏
	Status   string `gorm:"column:status;size:16" json:"status"`
	Sort     int    `gorm:"column:sort" json:"sort"`
	Remark   string `gorm:"column:remark;size:500" json:"remark"`
}

func (PaymentChannel) TableName() string { return "payment_channels" }

// IsEnabled 判断渠道是否启用。
func (c *PaymentChannel) IsEnabled() bool { return c.Status == ChannelEnabled }

// 支付日志事件类型。
const (
	PayEventCreate        = "create"          // 发起支付
	PayEventCreateFailed  = "create_failed"   // 发起支付失败
	PayEventNotify        = "notify"          // 收到回调
	PayEventNotifyInvalid = "notify_invalid"  // 回调验签失败
	PayEventPaid          = "paid"            // 支付成功并完成业务处理
	PayEventDuplicate     = "duplicate"       // 重复回调（已幂等跳过）
	PayEventAmountBad     = "amount_mismatch" // 金额不符
	PayEventQuery         = "query"           // 主动查询
	PayEventRefund        = "refund"          // 退款
	PayEventRefundFailed  = "refund_failed"
	PayEventManualRefund  = "manual_refund" // 管理员标记人工退款
)

// PaymentLog 是支付流水日志。
//
// request_data / response_data 落库前必须经过 SanitizeSensitive 过滤，
// 避免把商户密钥、私钥写进数据库。
type PaymentLog struct {
	ID           uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	OrderID      uint64    `gorm:"column:order_id;index" json:"order_id"`
	OrderNo      string    `gorm:"column:order_no;size:40" json:"order_no"`
	ChannelID    uint64    `gorm:"column:channel_id" json:"channel_id"`
	Provider     string    `gorm:"column:provider;size:32" json:"provider"`
	TradeNo      string    `gorm:"column:trade_no;size:128" json:"trade_no"`
	Event        string    `gorm:"column:event;size:48" json:"event"`
	Amount       int64     `gorm:"column:amount" json:"amount"`
	Status       string    `gorm:"column:status;size:32" json:"status"`
	RequestData  string    `gorm:"column:request_data" json:"request_data"`
	ResponseData string    `gorm:"column:response_data" json:"response_data"`
	ClientIP     string    `gorm:"column:client_ip;size:64" json:"client_ip"`
	CreatedAt    time.Time `gorm:"column:created_at" json:"created_at"`
}

func (PaymentLog) TableName() string { return "payment_logs" }
