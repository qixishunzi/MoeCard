// Package payment 定义支付渠道抽象与各平台 Adapter。
//
// **架构红线**：本包及其子包只负责
//   - 与支付平台通信
//   - 构造签名 / 验证签名
//   - 解析回调报文
//
// 绝不负责：发货、核销优惠券、扣库存、改订单状态。
// 那些属于 OrderService.HandlePaymentSuccess 的统一事务。
//
// 新增一个支付平台 = 在子包里实现 Provider 接口 + 在 registry 注册，
// 不需要改动任何业务代码。
package payment

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// 各 provider 的唯一标识。渠道表的 provider 字段存的就是这些值。
const (
	ProviderYipayV1 = "yipay_v1"
	ProviderYipayV2 = "yipay_v2"
	ProviderAlipay  = "alipay"
	ProviderWechat  = "wechat"
	ProviderStripe  = "stripe"
	ProviderHashPay = "hashpay"
)

// 前端拿到 PaymentResponse 后的处理方式。
const (
	ActionRedirect = "redirect" // 直接跳转 URL
	ActionQRCode   = "qrcode"   // 展示二维码（值为待编码的字符串）
	ActionForm     = "form"     // 渲染并自动提交 HTML 表单
)

// 常见错误。
var (
	ErrNotSupported     = errors.New("该支付渠道不支持此操作")
	ErrInvalidSignature = errors.New("签名校验失败")
	ErrInvalidConfig    = errors.New("支付渠道配置不完整")
	ErrNotPaid          = errors.New("订单尚未支付")
)

// PaymentRequest 是发起支付的入参。
//
// Amount 单位为最小货币单位（人民币 = 分）。各 Adapter 自行转换成平台要求的格式。
type PaymentRequest struct {
	OrderNo   string // 商户订单号
	Subject   string // 商品名称 / 订单标题
	Body      string // 商品描述
	Amount    int64  // 应付金额（分）
	Currency  string // ISO 4217，如 CNY / USD
	NotifyURL string // 异步回调地址（服务端）
	ReturnURL string // 同步跳转地址（浏览器）
	ClientIP  string
	Email     string
	Device    string         // pc | mobile，部分平台需要区分
	Extra     map[string]any // 平台特有的附加参数
}

// PaymentResponse 是发起支付的结果。
type PaymentResponse struct {
	Action   string // redirect | qrcode | form
	URL      string // Action=redirect 时的跳转地址
	QRCode   string // Action=qrcode 时待编码的内容
	FormHTML string // Action=form 时的自动提交表单
	TradeNo  string // 平台交易号（若此时已返回）
	Raw      string // 平台原始响应，落 payment_logs 用（已脱敏）
}

// NotifyRequest 封装收到的回调请求。
//
// 之所以把原始 Body 和 Header 都带上：不同平台的验签依赖不同的东西 ——
// 易支付验 query，微信验 header + body，Stripe 验 header + 原始 body 字节。
// 任何一步的重新序列化都会导致签名不匹配，所以必须保留原始字节。
type NotifyRequest struct {
	Method      string
	Header      http.Header
	Query       url.Values
	Form        url.Values // 已解析的 form-urlencoded body
	Body        []byte     // 原始 body 字节，签名校验必须用它
	ContentType string
	RemoteIP    string
	ChannelID   uint64
}

// NotifyResult 是验签解析后的结果。
//
// Success = true 只表示"这是一条来自平台的、签名有效的、表示支付成功的通知"。
// 是否真的给用户发货，由 OrderService 在事务内二次判定（金额、订单状态）。
type NotifyResult struct {
	Success  bool   // 签名有效且平台声明支付成功
	OrderNo  string // 商户订单号
	TradeNo  string // 平台交易号
	Amount   int64  // 平台声明的实付金额（分）
	Currency string
	Status   string         // 平台原始状态字符串
	Raw      map[string]any // 解析后的报文（已脱敏），落日志用

	// 应答内容。各平台要求不同：易支付要纯文本 "success"，
	// 微信要 JSON，Stripe 只要 200。由 Adapter 决定。
	ResponseBody        string
	ResponseContentType string
	ResponseStatus      int
}

// FailResponse 构造一个"验签失败"的应答。
func FailResponse(contentType, body string, status int) *NotifyResult {
	if status == 0 {
		status = http.StatusBadRequest
	}
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	return &NotifyResult{
		Success:             false,
		ResponseBody:        body,
		ResponseContentType: contentType,
		ResponseStatus:      status,
	}
}

// QueryRequest 是主动查询的入参。
//
// 之所以不只传商户订单号：部分平台（如 HashPay）的查询接口只认**平台订单号**，
// 商户订单号查不到。TradeNo 在创建支付时由平台返回并持久化到 orders.payment_trade_no。
type QueryRequest struct {
	OrderNo string // 商户订单号
	TradeNo string // 平台交易号，可能为空（订单从未发起过支付时）
}

// PaymentStatus 是主动查询的结果。
type PaymentStatus struct {
	Paid     bool
	TradeNo  string
	Amount   int64
	Currency string
	Status   string
	Raw      string
}

// RefundRequest 是退款入参。
type RefundRequest struct {
	OrderNo     string
	TradeNo     string
	RefundNo    string // 商户退款单号
	TotalAmount int64  // 订单总额（分）
	Amount      int64  // 本次退款额（分）
	Currency    string
	Reason      string
}

// RefundResult 是退款结果。
type RefundResult struct {
	Success  bool
	RefundNo string
	Amount   int64
	Status   string
	Raw      string
}

// ConfigField 描述一个配置项，用于驱动后台表单渲染与脱敏规则。
type ConfigField struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // text | password | textarea | select | number | switch
	Required    bool     `json:"required"`
	Secret      bool     `json:"secret"` // true 时出参脱敏、提交空值保留旧值
	Placeholder string   `json:"placeholder,omitempty"`
	Help        string   `json:"help,omitempty"`
	Options     []Option `json:"options,omitempty"`
	Default     string   `json:"default,omitempty"`
}

// Option 是 select 类型配置项的选项。
type Option struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Provider 是所有支付渠道必须实现的接口。
type Provider interface {
	// Key 返回 provider 唯一标识。
	Key() string

	// CreatePayment 向支付平台发起支付。
	CreatePayment(ctx context.Context, req PaymentRequest) (*PaymentResponse, error)

	// VerifyNotify 验证异步回调的真实性并解析出订单信息。
	//
	// 这是判定"这笔钱是否真的收到了"的**唯一**入口。
	// 实现必须完成完整的签名校验，绝不能因为报文里写着 status=success 就返回 Success=true。
	VerifyNotify(ctx context.Context, req NotifyRequest) (*NotifyResult, error)

	// QueryPayment 主动向平台查询订单支付状态（用于回调丢失时兜底）。
	QueryPayment(ctx context.Context, req QueryRequest) (*PaymentStatus, error)

	// Refund 发起退款。不支持时返回 ErrNotSupported，由业务层降级为人工退款。
	Refund(ctx context.Context, req RefundRequest) (*RefundResult, error)
}

// Factory 根据渠道配置构造一个 Provider 实例。
type Factory func(cfg map[string]string) (Provider, error)

// Descriptor 描述一个已注册的 provider。
type Descriptor struct {
	Key       string        `json:"key"`
	Name      string        `json:"name"`
	Fields    []ConfigField `json:"fields"`
	CanRefund bool          `json:"can_refund"`
	Note      string        `json:"note,omitempty"`
	factory   Factory
}
