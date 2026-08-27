// Package hashpay 实现 HashPay 加密货币支付网关。
//
// 对接依据：https://github.com/tgdash/hashpay （Cloudflare Workers + Hono + D1）
//
// # 协议要点
//
// 请求签名（src/server/services/merchants/index.ts）：
//
//	三个请求头：x-merchant-id / x-timestamp / x-signature
//	待签名串 = [METHOD, pathname+search, timestamp, body].join("\n")
//	算法     = RSASSA-PKCS1-v1_5 + SHA-256，结果标准 Base64
//	时间容差 = 300 秒
//
// 接口（src/server/http/routes/auth.ts，挂载在 /api 下）：
//
//	POST /api/merchant/new       创建订单（按 merchantNo 幂等，重复提交返回 reused:true）
//	GET  /api/order/:orderId     查询订单（:orderId 是 **HashPay 的订单 id**，不是 merchantNo）
//
// 回调（src/server/services/orders/notifications.ts）：
//
//	POST 到下单时传入的 callback 地址
//	头：x-hashpay-merchant / x-hashpay-timestamp / x-hashpay-encryption
//	体：{"alg":"RSA-OAEP-256+A256GCM","key":..,"iv":..,"data":..}
//	    key  = 用**商户公钥** RSA-OAEP(SHA-256) 加密的 AES-256 密钥
//	    iv   = 12 字节随机数
//	    data = AES-256-GCM 密文（认证标签附在尾部）
//	解密后 = {"timestamp":<unix>,"payload":{orderId,merchantNo,amount,currency,status,payment}}
//	商户返回任意 2xx 即视为接收成功；失败会退避重试，最多 8 次。
//
// # 一个必须知道的安全问题
//
// HashPay 的回调**只有加密、没有签名**。而"商户公钥"按定义不是秘密 ——
// 任何拿到它和回调地址的人都能自行构造一份合法的加密信封，伪造一条 "paid" 通知。
//
// 因此本 Adapter 在解密之后**默认再用签名请求回查一次订单**
// （GET /api/order/:orderId，只有持有商户私钥的人才能发出），
// 以服务端的权威结果为准。回调本身只当作"去查一下"的触发信号。
//
// 这个行为由 verify_by_query 配置控制，默认开启，**不建议关闭**。
//
// # 金额
//
// HashPay 的 orders.amount 是 SQL REAL，即**十进制小数**（如 10.5），
// 不是最小货币单位。本系统内部统一存"分"，因此出入都要换算，
// 且换算走字符串而非 float —— 见 amountToJSON / parseAmountNumber。
package hashpay

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/moecard/server/internal/payment"
	"github.com/moecard/server/internal/utils"
)

const (
	pathCreate = "/api/merchant/new"
	pathOrder  = "/api/order/"

	// callbackAlgorithm 是 HashPay 回调信封声明的算法标识。
	callbackAlgorithm = "RSA-OAEP-256+A256GCM"

	// 订单状态。初始为 pending，收款确认后为 paid。
	statusPaid    = "paid"
	statusPending = "pending"
)

func init() {
	payment.Register(payment.Descriptor{
		Key:       payment.ProviderHashPay,
		Name:      "HashPay（加密货币）",
		CanRefund: false,
		Note: "HashPay 加密货币支付网关（TRC20 / ERC20 / TON / Solana / Binance / OKX 等）。" +
			"需在 HashPay 商户后台登记本站的 RSA 公钥；私钥同时用于请求签名与回调解密。",
		Fields: []payment.ConfigField{
			{Key: "gateway", Label: "网关地址", Type: "text", Required: true,
				Placeholder: "https://pay.example.com",
				Help:        "HashPay 站点根地址，不要带 /api/merchant/new"},
			{Key: "merchant_id", Label: "商户 ID", Type: "text", Required: true,
				Help: "对应请求头 X-Merchant-Id"},
			{Key: "private_key", Label: "商户 RSA 私钥", Type: "textarea", Required: true, Secret: true,
				Help: "支持 PKCS#1 / PKCS#8 / 裸 base64。" +
					"配套的公钥需要登记到 HashPay 商户后台 —— 它既用于验证本站请求的签名，也用于加密回调。"},
			{Key: "currency", Label: "计价币种", Type: "text", Default: "CNY",
				Help: "订单的计价币种；用户实际支付哪种加密货币由 HashPay 收银台决定。留空则用 HashPay 默认币种"},
			{Key: "verify_by_query", Label: "回调后回查确认", Type: "switch", Default: "1",
				Help: "⚠️ 强烈建议保持开启。HashPay 回调只加密不签名，" +
					"关闭后任何知道你公钥的人都能伪造支付成功通知"},
		},
	}, New)
}

// Provider 是 HashPay 的实现。
type Provider struct {
	gateway       string
	merchantID    string
	privPEM       string
	currency      string
	verifyByQuery bool

	client *http.Client
}

// New 构造 Provider。
func New(cfg map[string]string) (payment.Provider, error) {
	gw := strings.TrimRight(strings.TrimSpace(cfg["gateway"]), "/")
	if gw == "" {
		return nil, fmt.Errorf("%w: 缺少网关地址", payment.ErrInvalidConfig)
	}
	if !strings.HasPrefix(gw, "http://") && !strings.HasPrefix(gw, "https://") {
		gw = "https://" + gw
	}

	p := &Provider{
		gateway:    gw,
		merchantID: strings.TrimSpace(cfg["merchant_id"]),
		privPEM:    strings.TrimSpace(cfg["private_key"]),
		currency:   strings.TrimSpace(cfg["currency"]),
		// 只有显式设为 0/false 才关闭，避免配置缺失时静默降级到不安全模式
		verifyByQuery: cfg["verify_by_query"] != "0" && !strings.EqualFold(cfg["verify_by_query"], "false"),
		client:        payment.NewHTTPClient(payment.DefaultTimeout),
	}

	// 启动即校验私钥，把格式错误挡在保存配置这一步
	if _, err := payment.ParsePrivateKey(p.privPEM); err != nil {
		return nil, fmt.Errorf("商户 RSA 私钥无效: %w", err)
	}
	return p, nil
}

// Key 返回 provider 标识。
func (p *Provider) Key() string { return payment.ProviderHashPay }

func (p *Provider) privateKey() (*rsa.PrivateKey, error) {
	return payment.ParsePrivateKey(p.privPEM)
}

// signedRequest 发起一个带 HashPay 签名头的请求。
//
// 待签名串严格按服务端实现拼接：
//
//	[METHOD, pathname+search, timestamp, body].join("\n")
//
// pathWithQuery 必须与真实请求的 URL 路径部分逐字符一致（含 query），
// 差一个斜杠或少一个参数都会验签失败。
func (p *Provider) signedRequest(ctx context.Context, method, pathWithQuery string, body []byte) (*payment.HTTPResult, error) {
	key, err := p.privateKey()
	if err != nil {
		return nil, err
	}

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	signPayload := strings.Join([]string{
		strings.ToUpper(method),
		pathWithQuery,
		ts,
		string(body),
	}, "\n")

	sig, err := payment.SignRSA(key, signPayload, "SHA256")
	if err != nil {
		return nil, err
	}

	headers := map[string]string{
		"X-Merchant-Id": p.merchantID,
		"X-Timestamp":   ts,
		"X-Signature":   sig,
		"Accept":        "application/json",
	}

	url := p.gateway + pathWithQuery
	if method == http.MethodGet {
		return payment.GetURL(ctx, p.client, url, nil, headers)
	}
	return payment.PostJSON(ctx, p.client, url, body, headers)
}

// hashpayError 解析 HashPay 的错误结构 {"error":{"key":"errors.xxx","params":{}}}。
func hashpayError(status int, body []byte) error {
	var e struct {
		Error struct {
			Key    string         `json:"key"`
			Params map[string]any `json:"params"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err == nil && e.Error.Key != "" {
		return fmt.Errorf("HashPay 接口失败 [HTTP %d] %s", status, e.Error.Key)
	}
	return fmt.Errorf("HashPay 接口失败 (HTTP %d): %s", status, truncate(string(body), 300))
}

// orderSummary 是 HashPay 返回的订单摘要。
//
// 字段名同时兼容 merchantOrderSummary 与 publicOrder 两个序列化器 ——
// 前者用 expiresAt，后者的原始对象用 expireAt。
type orderSummary struct {
	ID         string      `json:"id"`
	MerchantNo string      `json:"merchantNo"`
	Amount     json.Number `json:"amount"`
	Currency   string      `json:"currency"`
	Status     string      `json:"status"`
	ExpiresAt  int64       `json:"expiresAt"`
	ExpireAt   int64       `json:"expireAt"`
	PaidAt     int64       `json:"paidAt"`
}

// amountToJSON 把内部的"分"转成 HashPay 要的十进制数值。
//
// 用 json.RawMessage 直接写字面量（如 178.20）而不是先转 float64 —— float 在
// JSON 序列化时可能变成 178.19999999999999，服务端 Number() 后就对不上账了。
func amountToJSON(cents int64) json.RawMessage {
	return json.RawMessage(utils.AmountToYuanString(cents))
}

// parseAmountNumber 把 HashPay 返回的十进制金额转回"分"。
//
// 走字符串解析而非 float64：json.Number 保留了原始字面量，
// utils.ParseAmount 是纯字符串运算，全程不引入浮点误差。
func parseAmountNumber(n json.Number) (int64, error) {
	s := strings.TrimSpace(n.String())
	if s == "" {
		return 0, fmt.Errorf("金额为空")
	}
	return utils.ParseAmount(s)
}

// CreatePayment 创建订单。
//
// merchantNo 用本站订单号 —— HashPay 对 (merchant, merchant_no) 建了唯一索引，
// 重复提交同一个 merchantNo 会返回已有订单并带上 reused:true，
// 因此这个接口天然幂等，用户反复点"立即支付"不会产生多笔订单。
func (p *Provider) CreatePayment(ctx context.Context, req payment.PaymentRequest) (*payment.PaymentResponse, error) {
	if req.Amount <= 0 {
		return nil, fmt.Errorf("金额必须大于 0")
	}

	body := map[string]any{
		"merchantNo": req.OrderNo,
		"amount":     amountToJSON(req.Amount),
		"callback":   req.NotifyURL,
		"return_url": req.ReturnURL,
	}
	// currency 留空时由 HashPay 使用系统默认币种
	if cur := p.currency; cur != "" {
		body["currency"] = cur
	} else if req.Currency != "" {
		body["currency"] = req.Currency
	}
	if d := utils.TrimAndLimit(utils.StripHTML(req.Subject), 200); d != "" {
		body["description"] = d
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("构造请求体失败: %w", err)
	}

	res, err := p.signedRequest(ctx, http.MethodPost, pathCreate, raw)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, hashpayError(res.StatusCode, res.Body)
	}

	var out struct {
		CheckoutURL string       `json:"checkoutUrl"`
		Order       orderSummary `json:"order"`
		Reused      bool         `json:"reused"`
	}
	if err := json.Unmarshal(res.Body, &out); err != nil {
		return nil, fmt.Errorf("HashPay 返回无法解析 (HTTP %d): %s",
			res.StatusCode, truncate(string(res.Body), 300))
	}
	if out.CheckoutURL == "" {
		return nil, fmt.Errorf("HashPay 未返回 checkoutUrl（响应: %s）", truncate(string(res.Body), 200))
	}

	return &payment.PaymentResponse{
		Action: payment.ActionRedirect,
		URL:    out.CheckoutURL,
		// 平台订单 id 必须落库：GET /api/order/:orderId 只认它，不认 merchantNo
		TradeNo: out.Order.ID,
		Raw:     truncate(string(res.Body), 2000),
	}, nil
}

// callbackEnvelope 是回调的外层加密信封。
type callbackEnvelope struct {
	Alg  string `json:"alg"`
	Key  string `json:"key"`
	IV   string `json:"iv"`
	Data string `json:"data"`
}

// callbackPayload 是解密后的内容。
type callbackPayload struct {
	Timestamp int64 `json:"timestamp"`
	Payload   struct {
		OrderID    string          `json:"orderId"`
		MerchantNo string          `json:"merchantNo"`
		Amount     json.Number     `json:"amount"`
		Currency   string          `json:"currency"`
		Status     string          `json:"status"`
		Payment    json.RawMessage `json:"payment"`
	} `json:"payload"`
}

// ackResponse 是回调的应答。HashPay 只判断 response.ok（2xx），内容不限。
const ackResponse = `{"received":true}`

// VerifyNotify 解密并核实异步通知。
//
// 完整链条：
//  1. 商户号头必须与渠道配置一致
//  2. 信封算法标识必须匹配
//  3. RSA-OAEP(SHA-256) 解出 AES 密钥 —— 只有持私钥的人能做到
//  4. AES-256-GCM 解密，认证标签保证密文未被篡改
//  5. **回查订单**（默认开启）：用签名请求向 HashPay 确认状态与金额
//
// 第 5 步是关键。前 4 步只证明"这份密文是用我的公钥加密的"，
// 而公钥并不保密，所以单靠它无法证明来源。真正的authenticity 来自
// 只有我能发出的那个签名查询请求。
func (p *Provider) VerifyNotify(ctx context.Context, req payment.NotifyRequest) (*payment.NotifyResult, error) {
	fail := func(err error) (*payment.NotifyResult, error) {
		return payment.FailResponse("application/json", `{"received":false}`, http.StatusBadRequest), err
	}

	// 1. 商户号
	if got := req.Header.Get("X-HashPay-Merchant"); got != "" && got != p.merchantID {
		return fail(fmt.Errorf("%w: 回调商户号 %q 与渠道配置不一致", payment.ErrInvalidSignature, got))
	}

	// 2. 信封
	var env callbackEnvelope
	if err := json.Unmarshal(req.Body, &env); err != nil {
		return fail(fmt.Errorf("%w: 回调不是合法 JSON: %v", payment.ErrInvalidSignature, err))
	}
	if env.Alg != "" && env.Alg != callbackAlgorithm {
		return fail(fmt.Errorf("%w: 不支持的回调加密算法 %q", payment.ErrInvalidSignature, env.Alg))
	}
	if env.Key == "" || env.IV == "" || env.Data == "" {
		return fail(fmt.Errorf("%w: 回调信封缺少 key/iv/data", payment.ErrInvalidSignature))
	}

	// 3. 解出一次性 AES 密钥
	priv, err := p.privateKey()
	if err != nil {
		return payment.FailResponse("application/json", `{"received":false}`, http.StatusInternalServerError), err
	}
	aesKey, err := payment.DecryptRSAOAEP256(priv, env.Key)
	if err != nil {
		return fail(err)
	}

	// 4. 解密正文
	iv, err := base64.StdEncoding.DecodeString(env.IV)
	if err != nil {
		return fail(fmt.Errorf("%w: iv 不是合法 base64", payment.ErrInvalidSignature))
	}
	data, err := base64.StdEncoding.DecodeString(env.Data)
	if err != nil {
		return fail(fmt.Errorf("%w: data 不是合法 base64", payment.ErrInvalidSignature))
	}
	plain, err := payment.DecryptAESGCMRaw(aesKey, iv, data)
	if err != nil {
		return fail(err)
	}

	var cb callbackPayload
	dec := json.NewDecoder(strings.NewReader(string(plain)))
	dec.UseNumber()
	if err := dec.Decode(&cb); err != nil {
		return fail(fmt.Errorf("回调内容解析失败: %w", err))
	}

	orderNo := cb.Payload.MerchantNo
	tradeNo := cb.Payload.OrderID
	status := cb.Payload.Status
	amount, amtErr := parseAmountNumber(cb.Payload.Amount)
	currency := cb.Payload.Currency

	if orderNo == "" {
		return fail(fmt.Errorf("%w: 回调缺少 merchantNo", payment.ErrInvalidSignature))
	}

	// 5. 回查确认 —— 以服务端权威结果为准
	if p.verifyByQuery && tradeNo != "" {
		st, qErr := p.queryByOrderID(ctx, tradeNo)
		if qErr != nil {
			// 查不到就不能认账。返回错误让上层回 5xx，HashPay 会重试。
			return payment.FailResponse("application/json", `{"received":false}`, http.StatusInternalServerError),
				fmt.Errorf("回调回查失败，暂不确认支付: %w", qErr)
		}
		// 一切以回查结果为准，回调内容仅作参考
		status = st.Status
		if st.Amount > 0 {
			amount, amtErr = st.Amount, nil
		}
		if st.Currency != "" {
			currency = st.Currency
		}
	}

	if amtErr != nil {
		return fail(fmt.Errorf("回调金额格式错误: %w", amtErr))
	}

	return &payment.NotifyResult{
		Success:  strings.EqualFold(status, statusPaid),
		OrderNo:  orderNo,
		TradeNo:  tradeNo,
		Amount:   amount,
		Currency: currency,
		Status:   status,
		Raw: map[string]any{
			"orderId":    tradeNo,
			"merchantNo": orderNo,
			"status":     status,
			"currency":   currency,
			"timestamp":  cb.Timestamp,
			"verified":   p.verifyByQuery,
		},
		ResponseBody:        ackResponse,
		ResponseContentType: "application/json; charset=utf-8",
		ResponseStatus:      http.StatusOK,
	}, nil
}

// queryByOrderID 用签名请求查询订单。
func (p *Provider) queryByOrderID(ctx context.Context, orderID string) (*payment.PaymentStatus, error) {
	if strings.TrimSpace(orderID) == "" {
		return nil, fmt.Errorf("缺少 HashPay 订单号")
	}

	res, err := p.signedRequest(ctx, http.MethodGet, pathOrder+orderID, nil)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, hashpayError(res.StatusCode, res.Body)
	}

	// publicOrder 序列化器可能直接返回订单对象，也可能包一层
	var wrapper struct {
		Order *orderSummary `json:"order"`
	}
	var summary orderSummary

	dec := json.NewDecoder(strings.NewReader(string(res.Body)))
	dec.UseNumber()
	if err := dec.Decode(&summary); err != nil {
		return nil, fmt.Errorf("HashPay 查询返回无法解析: %s", truncate(string(res.Body), 300))
	}
	if summary.ID == "" {
		if err := json.Unmarshal(res.Body, &wrapper); err == nil && wrapper.Order != nil {
			summary = *wrapper.Order
		}
	}

	amount, _ := parseAmountNumber(summary.Amount)
	return &payment.PaymentStatus{
		Paid:     strings.EqualFold(summary.Status, statusPaid),
		TradeNo:  summary.ID,
		Amount:   amount,
		Currency: summary.Currency,
		Status:   summary.Status,
		Raw:      truncate(string(res.Body), 2000),
	}, nil
}

// QueryPayment 主动查询订单。
//
// HashPay 的查询接口只认平台订单号（GET /api/order/:orderId），
// 因此依赖创建支付时落库的 TradeNo。
func (p *Provider) QueryPayment(ctx context.Context, req payment.QueryRequest) (*payment.PaymentStatus, error) {
	if strings.TrimSpace(req.TradeNo) == "" {
		// 订单从未发起过支付，自然也就没有平台订单号
		return &payment.PaymentStatus{Paid: false, Status: statusPending}, nil
	}
	return p.queryByOrderID(ctx, req.TradeNo)
}

// Refund HashPay 未提供退款接口。
//
// 加密货币转账本身不可逆，退款需要人工向用户地址回款，
// 因此这里返回不支持，由后台降级为「人工退款」记账。
func (p *Provider) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResult, error) {
	return nil, payment.ErrNotSupported
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
