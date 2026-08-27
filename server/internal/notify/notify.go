// Package notify 提供商家侧的即时通知。
//
// 解决的问题：手动发货订单进来、卡密卖光、订单收了钱却发不出货 ——
// 这些都需要人立刻介入，但在此之前只能靠商家主动登后台刷新才能发现。
//
// 设计与 payment 包保持一致：Provider 接口 + 注册表 + 配置字段自描述，
// 后台界面因此不需要为每个渠道写一套表单，新增渠道也不用动前端。
package notify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// 事件类型。
const (
	EventOrderPaid      = "order_paid"      // 订单支付成功
	EventManualDelivery = "manual_delivery" // 需要人工发货
	EventNeedsAttention = "needs_attention" // 收了钱但发不出货，必须人工处理
	EventLowStock       = "low_stock"       // 卡密库存告急
	EventRefund         = "refund"          // 发生退款
	EventTest           = "test"            // 后台点「发送测试」
)

// EventLabels 是事件的中文名，用于后台展示与消息标题。
var EventLabels = map[string]string{
	EventOrderPaid:      "订单支付成功",
	EventManualDelivery: "待人工发货",
	EventNeedsAttention: "订单需要处理",
	EventLowStock:       "库存告急",
	EventRefund:         "订单退款",
	EventTest:           "测试通知",
}

// Priority 表示消息紧急程度，供支持分级的渠道（如 Bark）使用。
type Priority string

const (
	PriorityNormal Priority = "normal"
	PriorityUrgent Priority = "urgent"
)

// Message 是一条待发送的通知。
type Message struct {
	Event    string
	Title    string
	Body     string
	Priority Priority
	// URL 是点击通知后跳转的地址（一般是后台订单详情）。
	URL string
	// Fields 是结构化明细，渠道自行决定如何排版。
	Fields []Field
}

// Field 是消息里的一行键值明细。
type Field struct {
	Key   string
	Value string
}

// Text 把消息渲染成纯文本，供不支持富格式的渠道使用。
func (m *Message) Text() string {
	var sb strings.Builder
	sb.WriteString(m.Title)
	if m.Body != "" {
		sb.WriteString("\n")
		sb.WriteString(m.Body)
	}
	for _, f := range m.Fields {
		sb.WriteString("\n")
		sb.WriteString(f.Key)
		sb.WriteString("：")
		sb.WriteString(f.Value)
	}
	if m.URL != "" {
		sb.WriteString("\n")
		sb.WriteString(m.URL)
	}
	return sb.String()
}

// ConfigField 描述渠道的一个配置项，字段含义与 payment.ConfigField 一致。
type ConfigField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"` // text | password | switch
	Required    bool   `json:"required"`
	Secret      bool   `json:"secret"`
	Placeholder string `json:"placeholder,omitempty"`
	Help        string `json:"help,omitempty"`
}

// Provider 是通知渠道接口。
type Provider interface {
	Key() string
	// Send 投递一条消息。返回 error 即视为失败，由上层记录到 notify_logs。
	Send(ctx context.Context, msg *Message) error
}

// MailSender 由上层注入，用于邮件渠道。
//
// 定义成接口而不是直接依赖 mail 包：notify 只关心"能不能把一封信发出去"，
// 不该知道 SMTP 配置存在哪、怎么读。
type MailSender interface {
	// Ready 表示 SMTP 是否已配置且启用。
	Ready() bool
	// SendNotify 发送一封通知邮件。
	SendNotify(ctx context.Context, to, subject, htmlBody string) error
	// SiteName 用于邮件标题与页眉。
	SiteName() string
}

// Deps 是构造渠道时可用的外部依赖。
//
// 显式传参而不是用包级全局变量：全局变量会让"忘了注入"变成运行期才暴露的空指针，
// 而且测试之间会互相污染。
type Deps struct {
	Mail MailSender
}

// Factory 根据配置构造渠道实例。
type Factory func(cfg map[string]string, deps Deps) (Provider, error)

// Descriptor 描述一个可用渠道。
type Descriptor struct {
	Key     string        `json:"key"`
	Name    string        `json:"name"`
	Fields  []ConfigField `json:"fields"`
	Note    string        `json:"note,omitempty"`
	factory Factory
}

var (
	regMu    sync.RWMutex
	registry = map[string]*Descriptor{}
)

// Register 注册一个通知渠道。
func Register(d Descriptor, f Factory) {
	regMu.Lock()
	defer regMu.Unlock()
	d.factory = f
	registry[d.Key] = &d
}

// Descriptors 返回全部渠道描述（按 key 排序，保证后台展示顺序稳定）。
func Descriptors() []Descriptor {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Descriptor, 0, len(registry))
	for _, d := range registry {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// ErrUnknownProvider 表示渠道不存在。
var ErrUnknownProvider = errors.New("未知的通知渠道")

// Build 构造渠道实例。
func Build(key string, cfg map[string]string, deps Deps) (Provider, error) {
	regMu.RLock()
	d, ok := registry[key]
	regMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, key)
	}
	return d.factory(cfg, deps)
}

// httpClient 是所有渠道共用的 HTTP 客户端。
//
// 超时必须短：通知是旁路，卡在这里会让 goroutine 越堆越多。
// 5 秒发不出去就认为失败并记日志，业务侧不受任何影响。
var httpClient = &http.Client{Timeout: 5 * time.Second}

// Client 供各渠道实现使用。
func Client() *http.Client { return httpClient }
