// Package stripe 实现 Stripe Checkout 支付。
//
// 协议要点：
//   - 创建会话 POST https://api.stripe.com/v1/checkout/sessions（form-urlencoded）
//     鉴权 Authorization: Bearer sk_live_xxx
//   - **支付结果以 Webhook 为准**，绝不能只依赖用户支付完的前端跳转 ——
//     用户完全可以不回跳，也可以伪造回跳地址
//   - Webhook 验签：Stripe-Signature 头形如 t=<ts>,v1=<sig>
//     signed_payload = t + "." + 原始请求体
//     expected = HMAC-SHA256(webhook_secret, signed_payload) 的十六进制
//   - 时间戳容差 5 分钟，防重放
//
// 金额：Stripe 的 unit_amount 是目标币种的最小单位。
// 本系统内部同样以最小单位存储，因此对 CNY/USD 这类两位小数币种是 1:1；
// 对 JPY 这类零小数币种由 utils.AmountToStripeUnit 处理。
// **不做汇率换算** —— 汇率转换会引入舍入误差，导致回调金额校验失败或产生资损。
// 需要多币种时请为每种币种单独建一个渠道。
package stripe

import (
	"context"
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
	apiBase          = "https://api.stripe.com"
	pathSessions     = "/v1/checkout/sessions"
	pathRefunds      = "/v1/refunds"
	pathPISearch     = "/v1/payment_intents/search"
	webhookTolerance = 5 * time.Minute
)

func init() {
	payment.Register(payment.Descriptor{
		Key:       payment.ProviderStripe,
		Name:      "Stripe",
		CanRefund: true,
		Note:      "Stripe Checkout。必须在 Stripe Dashboard 配置 Webhook 指向本系统的回调地址，并监听 checkout.session.completed 事件。",
		Fields: []payment.ConfigField{
			{Key: "secret_key", Label: "Secret Key", Type: "password", Required: true, Secret: true,
				Placeholder: "sk_live_..."},
			{Key: "publishable_key", Label: "Publishable Key", Type: "text",
				Placeholder: "pk_live_...", Help: "可选，仅用于前端集成"},
			{Key: "webhook_secret", Label: "Webhook Secret", Type: "password", Required: true, Secret: true,
				Placeholder: "whsec_...", Help: "在 Dashboard → Developers → Webhooks 中获取"},
			{Key: "currency", Label: "结算币种", Type: "text", Default: "usd",
				Help: "ISO 4217 小写，如 usd / cny / eur。金额数值不做汇率换算，请确保与商城币种一致"},
		},
	}, New)
}

// Provider 是 Stripe 的实现。
type Provider struct {
	secretKey     string
	publishable   string
	webhookSecret string
	currency      string
	client        *http.Client
}

// New 构造 Provider。
func New(cfg map[string]string) (payment.Provider, error) {
	cur := strings.ToLower(strings.TrimSpace(cfg["currency"]))
	if cur == "" {
		cur = "usd"
	}
	return &Provider{
		secretKey:     strings.TrimSpace(cfg["secret_key"]),
		publishable:   strings.TrimSpace(cfg["publishable_key"]),
		webhookSecret: strings.TrimSpace(cfg["webhook_secret"]),
		currency:      cur,
		client:        payment.NewHTTPClient(payment.DefaultTimeout),
	}, nil
}

// Key 返回 provider 标识。
func (p *Provider) Key() string { return payment.ProviderStripe }

func (p *Provider) authHeaders() map[string]string {
	return map[string]string{
		"Authorization":  "Bearer " + p.secretKey,
		"Stripe-Version": "2024-06-20",
	}
}

func (p *Provider) postForm(ctx context.Context, path string, params map[string]string) ([]byte, int, error) {
	body := payment.MapToValues(params).Encode()
	req, err := http.NewRequest(http.MethodPost, apiBase+path, strings.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", payment.UserAgent)
	for k, v := range p.authHeaders() {
		req.Header.Set(k, v)
	}
	res, err := payment.DoRequest(ctx, p.client, req)
	if err != nil {
		return nil, 0, err
	}
	return res.Body, res.StatusCode, nil
}

func stripeError(status int, body []byte) error {
	var e struct {
		Error struct {
			Type    string `json:"type"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	if e.Error.Message != "" {
		return fmt.Errorf("Stripe 接口失败 [%d %s] %s", status, e.Error.Code, e.Error.Message)
	}
	return fmt.Errorf("Stripe 接口失败 (HTTP %d): %s", status, truncate(string(body), 300))
}

// CreatePayment 创建 Checkout Session。
func (p *Provider) CreatePayment(ctx context.Context, req payment.PaymentRequest) (*payment.PaymentResponse, error) {
	unitAmount := utils.AmountToStripeUnit(req.Amount, p.currency)
	if unitAmount <= 0 {
		return nil, fmt.Errorf("金额必须大于 0")
	}

	params := map[string]string{
		"mode":                "payment",
		"success_url":         req.ReturnURL,
		"cancel_url":          req.ReturnURL,
		"client_reference_id": req.OrderNo,

		"line_items[0][quantity]":                       "1",
		"line_items[0][price_data][currency]":           p.currency,
		"line_items[0][price_data][unit_amount]":        strconv.FormatInt(unitAmount, 10),
		"line_items[0][price_data][product_data][name]": utils.TrimAndLimit(req.Subject, 120),

		"metadata[order_no]": req.OrderNo,
		// 同时把订单号写到 PaymentIntent 的 metadata 上，
		// 这样主动查询时可以用 Search API 按 metadata 反查
		"payment_intent_data[metadata][order_no]": req.OrderNo,
	}
	if req.Email != "" {
		params["customer_email"] = req.Email
	}
	if req.Body != "" {
		if d := utils.TrimAndLimit(utils.StripHTML(req.Body), 200); d != "" {
			params["line_items[0][price_data][product_data][description]"] = d
		}
	}
	// Checkout Session 的过期时间（Stripe 要求 30 分钟 ~ 24 小时）
	if req.Extra != nil {
		if exp, ok := req.Extra["expire_at"].(time.Time); ok && !exp.IsZero() {
			if d := time.Until(exp); d >= 30*time.Minute && d <= 24*time.Hour {
				params["expires_at"] = strconv.FormatInt(exp.Unix(), 10)
			}
		}
	}

	body, status, err := p.postForm(ctx, pathSessions, params)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, stripeError(status, body)
	}

	var out struct {
		ID            string `json:"id"`
		URL           string `json:"url"`
		PaymentIntent string `json:"payment_intent"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("Stripe 返回无法解析: %s", truncate(string(body), 300))
	}
	if out.URL == "" {
		return nil, fmt.Errorf("Stripe 未返回 Checkout 地址")
	}
	return &payment.PaymentResponse{
		Action:  payment.ActionRedirect,
		URL:     out.URL,
		TradeNo: out.ID,
		Raw:     fmt.Sprintf(`{"session_id":%q,"payment_intent":%q}`, out.ID, out.PaymentIntent),
	}, nil
}

// stripeEvent 是 Webhook 事件外层结构。
type stripeEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

// checkoutSession 是 checkout.session.* 事件的对象。
type checkoutSession struct {
	ID                string            `json:"id"`
	ClientReferenceID string            `json:"client_reference_id"`
	AmountTotal       int64             `json:"amount_total"`
	Currency          string            `json:"currency"`
	PaymentStatus     string            `json:"payment_status"`
	Status            string            `json:"status"`
	PaymentIntent     string            `json:"payment_intent"`
	Metadata          map[string]string `json:"metadata"`
}

// VerifyNotify 校验 Webhook 签名并解析事件。
func (p *Provider) VerifyNotify(ctx context.Context, req payment.NotifyRequest) (*payment.NotifyResult, error) {
	sigHeader := req.Header.Get("Stripe-Signature")
	if sigHeader == "" {
		return payment.FailResponse("text/plain", "missing signature", http.StatusBadRequest),
			fmt.Errorf("%w: 缺少 Stripe-Signature 头", payment.ErrInvalidSignature)
	}
	if err := p.verifySignature(sigHeader, req.Body); err != nil {
		return payment.FailResponse("text/plain", "invalid signature", http.StatusBadRequest), err
	}

	var ev stripeEvent
	if err := json.Unmarshal(req.Body, &ev); err != nil {
		return payment.FailResponse("text/plain", "bad payload", http.StatusBadRequest),
			fmt.Errorf("Stripe 事件解析失败: %w", err)
	}

	okResp := &payment.NotifyResult{
		ResponseBody:        `{"received":true}`,
		ResponseContentType: "application/json",
		ResponseStatus:      http.StatusOK,
		Raw:                 map[string]any{"event_id": ev.ID, "event_type": ev.Type},
	}

	// 只有这两个事件代表"钱已到账"。其余事件（如 session.expired）
	// 一律回 200 让 Stripe 停止重试，但不触发任何业务处理。
	if ev.Type != "checkout.session.completed" && ev.Type != "checkout.session.async_payment_succeeded" {
		return okResp, nil
	}

	var s checkoutSession
	if err := json.Unmarshal(ev.Data.Object, &s); err != nil {
		return payment.FailResponse("text/plain", "bad object", http.StatusBadRequest),
			fmt.Errorf("Stripe session 解析失败: %w", err)
	}

	orderNo := s.ClientReferenceID
	if orderNo == "" && s.Metadata != nil {
		orderNo = s.Metadata["order_no"]
	}
	tradeNo := s.PaymentIntent
	if tradeNo == "" {
		tradeNo = s.ID
	}

	okResp.Success = s.PaymentStatus == "paid"
	okResp.OrderNo = orderNo
	okResp.TradeNo = tradeNo
	okResp.Amount = utils.StripeUnitToAmount(s.AmountTotal, s.Currency)
	okResp.Currency = strings.ToUpper(s.Currency)
	okResp.Status = s.PaymentStatus
	okResp.Raw["order_no"] = orderNo
	okResp.Raw["payment_status"] = s.PaymentStatus
	okResp.Raw["amount_total"] = s.AmountTotal
	okResp.Raw["currency"] = s.Currency
	return okResp, nil
}

// verifySignature 校验 Stripe-Signature。
//
// 格式：t=1614556800,v1=abc...,v1=def...（可能有多个 v1，任一匹配即可，
// 这是 Stripe 轮换 secret 期间的正常情况）
func (p *Provider) verifySignature(header string, body []byte) error {
	var (
		tsStr string
		sigs  []string
	)
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			tsStr = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	if tsStr == "" || len(sigs) == 0 {
		return fmt.Errorf("%w: Stripe-Signature 格式不正确", payment.ErrInvalidSignature)
	}

	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: 时间戳格式错误", payment.ErrInvalidSignature)
	}
	if diff := time.Since(time.Unix(ts, 0)); diff > webhookTolerance || diff < -webhookTolerance {
		return fmt.Errorf("%w: Webhook 时间戳超出允许窗口（可能为重放攻击）", payment.ErrInvalidSignature)
	}

	expected := payment.HMACSHA256Hex(p.webhookSecret, tsStr+"."+string(body))
	for _, s := range sigs {
		if payment.SecureCompareHex(s, expected) {
			return nil
		}
	}
	return fmt.Errorf("%w: Stripe Webhook 签名不匹配", payment.ErrInvalidSignature)
}

// QueryPayment 通过 PaymentIntent Search API 按 metadata.order_no 反查。
func (p *Provider) QueryPayment(ctx context.Context, req payment.QueryRequest) (*payment.PaymentStatus, error) {
	orderNo := req.OrderNo

	query := fmt.Sprintf(`metadata["order_no"]:"%s"`, strings.ReplaceAll(orderNo, `"`, ""))
	res, err := payment.GetURL(ctx, p.client, apiBase+pathPISearch,
		map[string]string{"query": query, "limit": "1"}, p.authHeaders())
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, stripeError(res.StatusCode, res.Body)
	}

	var out struct {
		Data []struct {
			ID       string `json:"id"`
			Amount   int64  `json:"amount"`
			Currency string `json:"currency"`
			Status   string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body, &out); err != nil {
		return nil, fmt.Errorf("Stripe 查询返回无法解析: %s", truncate(string(res.Body), 300))
	}
	if len(out.Data) == 0 {
		return &payment.PaymentStatus{Paid: false, Status: "not_found"}, nil
	}
	pi := out.Data[0]
	return &payment.PaymentStatus{
		Paid:     pi.Status == "succeeded",
		TradeNo:  pi.ID,
		Amount:   utils.StripeUnitToAmount(pi.Amount, pi.Currency),
		Currency: strings.ToUpper(pi.Currency),
		Status:   pi.Status,
		Raw:      truncate(string(res.Body), 2000),
	}, nil
}

// Refund 发起退款。
func (p *Provider) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResult, error) {
	if req.TradeNo == "" {
		return nil, fmt.Errorf("Stripe 退款需要 PaymentIntent ID")
	}
	params := map[string]string{
		"payment_intent": req.TradeNo,
		"amount":         strconv.FormatInt(utils.AmountToStripeUnit(req.Amount, p.currency), 10),
	}
	if req.RefundNo != "" {
		params["metadata[refund_no]"] = req.RefundNo
	}
	if req.Reason != "" {
		params["metadata[reason]"] = utils.TrimAndLimit(req.Reason, 200)
	}

	body, status, err := p.postForm(ctx, pathRefunds, params)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, stripeError(status, body)
	}
	var out struct {
		ID       string `json:"id"`
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("Stripe 退款返回无法解析: %s", truncate(string(body), 300))
	}
	return &payment.RefundResult{
		Success:  out.Status == "succeeded" || out.Status == "pending",
		RefundNo: out.ID,
		Amount:   utils.StripeUnitToAmount(out.Amount, out.Currency),
		Status:   out.Status,
		Raw:      truncate(string(body), 2000),
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
