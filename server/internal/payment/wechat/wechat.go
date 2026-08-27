// Package wechat 实现微信支付 APIv3。
//
// 协议要点：
//   - 所有请求带 Authorization: WECHATPAY2-SHA256-RSA2048 ...
//     签名串 = HTTP方法\nURL路径\n时间戳\n随机串\n请求报文主体\n
//     用商户 API 私钥做 SHA256withRSA，结果 base64
//   - 回调是 JSON，敏感数据在 resource 中用 AES-256-GCM 加密，
//     密钥为 APIv3 密钥（32 字节）
//   - 回调验签用微信支付平台证书的公钥，
//     验签串 = Wechatpay-Timestamp\nWechatpay-Nonce\n请求体\n
//   - 金额单位为**分**（与本系统内部一致，无需换算）
//   - 商户需应答 {"code":"SUCCESS","message":"成功"}
package wechat

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
	apiBase = "https://api.mch.weixin.qq.com"

	pathNative = "/v3/pay/transactions/native"
	pathH5     = "/v3/pay/transactions/h5"
	pathJSAPI  = "/v3/pay/transactions/jsapi"
	pathRefund = "/v3/refund/domestic/refunds"

	// notifyTimeWindow 是回调时间戳的允许偏差。
	// 超出窗口的请求视为重放攻击直接拒绝。
	notifyTimeWindow = 5 * time.Minute
)

func init() {
	payment.Register(payment.Descriptor{
		Key:       payment.ProviderWechat,
		Name:      "微信支付（官方）",
		CanRefund: true,
		Note:      "微信支付 APIv3。需在商户平台下载 API 证书并设置 APIv3 密钥。",
		Fields: []payment.ConfigField{
			{Key: "mch_id", Label: "商户号 mchid", Type: "text", Required: true},
			{Key: "app_id", Label: "AppID", Type: "text", Required: true,
				Help: "公众号 / 小程序 / 开放平台应用的 AppID，需与商户号绑定"},
			{Key: "merchant_serial_no", Label: "商户证书序列号", Type: "text", Required: true},
			{Key: "merchant_private_key", Label: "商户 API 私钥", Type: "textarea", Required: true, Secret: true,
				Help: "apiclient_key.pem 的完整内容"},
			{Key: "api_v3_key", Label: "APIv3 密钥", Type: "password", Required: true, Secret: true,
				Help: "32 位字符串，用于解密回调报文"},
			{Key: "platform_certificate", Label: "微信支付平台证书", Type: "textarea", Required: true, Secret: true,
				Help: "用于验证回调签名。可用微信官方 CertificateDownloader 工具下载 wechatpay_xxx.pem"},
			{Key: "pay_method", Label: "支付方式", Type: "select", Default: "native",
				Options: []payment.Option{
					{Label: "Native 扫码支付", Value: "native"},
					{Label: "H5 支付（手机浏览器）", Value: "h5"},
					{Label: "自动（PC 扫码 / 手机 H5）", Value: "auto"},
				}},
		},
	}, New)
}

// Provider 是微信支付的实现。
type Provider struct {
	mchID      string
	appID      string
	serialNo   string
	privPEM    string
	apiV3Key   string
	platCert   string
	platSerial string
	payMethod  string
	client     *http.Client
}

// New 构造 Provider。
func New(cfg map[string]string) (payment.Provider, error) {
	p := &Provider{
		mchID:     strings.TrimSpace(cfg["mch_id"]),
		appID:     strings.TrimSpace(cfg["app_id"]),
		serialNo:  strings.TrimSpace(cfg["merchant_serial_no"]),
		privPEM:   strings.TrimSpace(cfg["merchant_private_key"]),
		apiV3Key:  strings.TrimSpace(cfg["api_v3_key"]),
		platCert:  strings.TrimSpace(cfg["platform_certificate"]),
		payMethod: strings.TrimSpace(cfg["pay_method"]),
		client:    payment.NewHTTPClient(payment.DefaultTimeout),
	}
	if p.payMethod == "" {
		p.payMethod = "native"
	}
	if len(p.apiV3Key) != 32 {
		return nil, fmt.Errorf("%w: APIv3 密钥必须为 32 位字符，当前 %d 位", payment.ErrInvalidConfig, len(p.apiV3Key))
	}
	if _, err := payment.ParsePrivateKey(p.privPEM); err != nil {
		return nil, fmt.Errorf("商户 API 私钥无效: %w", err)
	}
	cert, err := payment.ParseCertificate(p.platCert)
	if err != nil {
		return nil, fmt.Errorf("微信支付平台证书无效: %w", err)
	}
	// 记录平台证书序列号，回调时用它比对 Wechatpay-Serial，
	// 平台轮换证书后能立刻发现"证书已过期需要更新"，而不是一直静默验签失败。
	p.platSerial = strings.ToUpper(cert.SerialNumber.Text(16))
	return p, nil
}

// Key 返回 provider 标识。
func (p *Provider) Key() string { return payment.ProviderWechat }

// authorization 构造 APIv3 的 Authorization 头。
func (p *Provider) authorization(method, urlPath, body string) (string, error) {
	key, err := payment.ParsePrivateKey(p.privPEM)
	if err != nil {
		return "", err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := payment.RandomNonce()
	message := method + "\n" + urlPath + "\n" + ts + "\n" + nonce + "\n" + body + "\n"

	sig, err := payment.SignRSA(key, message, "SHA256")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",signature="%s",timestamp="%s",serial_no="%s"`,
		p.mchID, nonce, sig, ts, p.serialNo), nil
}

func (p *Provider) doPost(ctx context.Context, urlPath string, payload any) ([]byte, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, fmt.Errorf("构造请求体失败: %w", err)
	}
	auth, err := p.authorization(http.MethodPost, urlPath, string(body))
	if err != nil {
		return nil, 0, err
	}
	res, err := payment.PostJSON(ctx, p.client, apiBase+urlPath, body, map[string]string{
		"Authorization": auth,
		"Accept":        "application/json",
	})
	if err != nil {
		return nil, 0, err
	}
	return res.Body, res.StatusCode, nil
}

func (p *Provider) doGet(ctx context.Context, urlPath string) ([]byte, int, error) {
	auth, err := p.authorization(http.MethodGet, urlPath, "")
	if err != nil {
		return nil, 0, err
	}
	res, err := payment.GetURL(ctx, p.client, apiBase+urlPath, nil, map[string]string{
		"Authorization": auth,
		"Accept":        "application/json",
	})
	if err != nil {
		return nil, 0, err
	}
	return res.Body, res.StatusCode, nil
}

// wechatError 解析微信返回的错误结构。
func wechatError(status int, body []byte) error {
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &e)
	if e.Code != "" {
		return fmt.Errorf("微信支付接口失败 [%d %s] %s", status, e.Code, e.Message)
	}
	return fmt.Errorf("微信支付接口失败 (HTTP %d): %s", status, truncate(string(body), 300))
}

// CreatePayment 发起支付。
func (p *Provider) CreatePayment(ctx context.Context, req payment.PaymentRequest) (*payment.PaymentResponse, error) {
	useH5 := p.payMethod == "h5" || (p.payMethod == "auto" && req.Device == "mobile")

	body := map[string]any{
		"appid":        p.appID,
		"mchid":        p.mchID,
		"description":  utils.TrimAndLimit(req.Subject, 120),
		"out_trade_no": req.OrderNo,
		"notify_url":   req.NotifyURL,
		"amount": map[string]any{
			// 微信的 total 单位就是分，与本系统内部单位一致，无需换算
			"total":    req.Amount,
			"currency": "CNY",
		},
	}
	// 订单在微信侧的失效时间，避免用户在我们这边已过期后仍能付款
	if req.Extra != nil {
		if exp, ok := req.Extra["expire_at"].(time.Time); ok && !exp.IsZero() {
			body["time_expire"] = exp.In(utils.LoadLocation("Asia/Shanghai")).Format(time.RFC3339)
		}
	}

	urlPath := pathNative
	if useH5 {
		urlPath = pathH5
		ip := req.ClientIP
		if ip == "" {
			ip = "127.0.0.1"
		}
		body["scene_info"] = map[string]any{
			"payer_client_ip": ip,
			"h5_info":         map[string]any{"type": "Wap"},
		}
	}

	respBody, status, err := p.doPost(ctx, urlPath, body)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, wechatError(status, respBody)
	}

	var out struct {
		CodeURL string `json:"code_url"`
		H5URL   string `json:"h5_url"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("微信支付返回无法解析: %s", truncate(string(respBody), 300))
	}

	if useH5 {
		if out.H5URL == "" {
			return nil, fmt.Errorf("微信支付未返回 h5_url")
		}
		u := out.H5URL
		if req.ReturnURL != "" {
			// redirect_url 让用户支付完自动回到我们的结果页
			u = payment.BuildQueryURL(u, map[string]string{"redirect_url": req.ReturnURL})
		}
		return &payment.PaymentResponse{Action: payment.ActionRedirect, URL: u,
			Raw: truncate(string(respBody), 1000)}, nil
	}
	if out.CodeURL == "" {
		return nil, fmt.Errorf("微信支付未返回 code_url")
	}
	return &payment.PaymentResponse{Action: payment.ActionQRCode, QRCode: out.CodeURL,
		Raw: truncate(string(respBody), 1000)}, nil
}

// notifyEnvelope 是回调外层结构。
type notifyEnvelope struct {
	ID           string `json:"id"`
	CreateTime   string `json:"create_time"`
	EventType    string `json:"event_type"`
	ResourceType string `json:"resource_type"`
	Summary      string `json:"summary"`
	Resource     struct {
		Algorithm      string `json:"algorithm"`
		Ciphertext     string `json:"ciphertext"`
		AssociatedData string `json:"associated_data"`
		Nonce          string `json:"nonce"`
		OriginalType   string `json:"original_type"`
	} `json:"resource"`
}

// transactionResource 是解密后的交易信息。
type transactionResource struct {
	MchID         string `json:"mchid"`
	AppID         string `json:"appid"`
	OutTradeNo    string `json:"out_trade_no"`
	TransactionID string `json:"transaction_id"`
	TradeState    string `json:"trade_state"`
	TradeStateDsc string `json:"trade_state_desc"`
	SuccessTime   string `json:"success_time"`
	Amount        struct {
		Total      int64  `json:"total"`
		PayerTotal int64  `json:"payer_total"`
		Currency   string `json:"currency"`
	} `json:"amount"`
}

var wechatFailResponse = `{"code":"FAIL","message":"验签失败"}`

// VerifyNotify 校验并解密异步通知。
//
// 完整校验链（缺一不可）：
//  1. 时间戳在 5 分钟窗口内 → 防重放
//  2. 平台证书序列号匹配   → 防止用旧证书伪造
//  3. RSA-SHA256 验签      → 证明报文来自微信
//  4. AES-256-GCM 解密     → GCM 的认证标签同时保证密文未被篡改
//  5. mchid / appid 匹配   → 防止别人用自己的商户号给我们发通知
func (p *Provider) VerifyNotify(ctx context.Context, req payment.NotifyRequest) (*payment.NotifyResult, error) {
	ts := req.Header.Get("Wechatpay-Timestamp")
	nonce := req.Header.Get("Wechatpay-Nonce")
	signature := req.Header.Get("Wechatpay-Signature")
	serial := req.Header.Get("Wechatpay-Serial")

	if ts == "" || nonce == "" || signature == "" {
		return payment.FailResponse("application/json", wechatFailResponse, http.StatusBadRequest),
			fmt.Errorf("%w: 缺少微信支付签名头", payment.ErrInvalidSignature)
	}

	// 1. 时间窗口
	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return payment.FailResponse("application/json", wechatFailResponse, http.StatusBadRequest),
			fmt.Errorf("%w: 时间戳格式错误", payment.ErrInvalidSignature)
	}
	if diff := time.Since(time.Unix(tsInt, 0)); diff > notifyTimeWindow || diff < -notifyTimeWindow {
		return payment.FailResponse("application/json", wechatFailResponse, http.StatusBadRequest),
			fmt.Errorf("%w: 回调时间戳超出允许窗口（可能为重放攻击）", payment.ErrInvalidSignature)
	}

	// 2. 证书序列号
	if serial != "" && p.platSerial != "" && !strings.EqualFold(serial, p.platSerial) {
		return payment.FailResponse("application/json", wechatFailResponse, http.StatusBadRequest),
			fmt.Errorf("%w: 平台证书序列号不匹配（微信可能已轮换证书，请在后台更新平台证书）", payment.ErrInvalidSignature)
	}

	// 3. 验签。注意必须用**原始 body 字节**，任何重新序列化都会导致签名不匹配。
	cert, err := payment.ParseCertificate(p.platCert)
	if err != nil {
		return payment.FailResponse("application/json", wechatFailResponse, http.StatusInternalServerError), err
	}
	pub, err := payment.ParsePublicKey(p.platCert)
	if err != nil {
		return payment.FailResponse("application/json", wechatFailResponse, http.StatusInternalServerError), err
	}
	_ = cert
	message := ts + "\n" + nonce + "\n" + string(req.Body) + "\n"
	if err := payment.VerifyRSA(pub, message, signature, "SHA256"); err != nil {
		return payment.FailResponse("application/json", wechatFailResponse, http.StatusBadRequest),
			fmt.Errorf("微信支付回调验签失败: %w", err)
	}

	var env notifyEnvelope
	if err := json.Unmarshal(req.Body, &env); err != nil {
		return payment.FailResponse("application/json", wechatFailResponse, http.StatusBadRequest),
			fmt.Errorf("回调报文解析失败: %w", err)
	}

	// 4. 解密
	plain, err := payment.DecryptAESGCM(p.apiV3Key, env.Resource.Nonce, env.Resource.AssociatedData, env.Resource.Ciphertext)
	if err != nil {
		return payment.FailResponse("application/json", wechatFailResponse, http.StatusBadRequest), err
	}
	var tr transactionResource
	if err := json.Unmarshal(plain, &tr); err != nil {
		return payment.FailResponse("application/json", wechatFailResponse, http.StatusBadRequest),
			fmt.Errorf("交易信息解析失败: %w", err)
	}

	// 5. 商户号 / AppID 校验
	if tr.MchID != "" && tr.MchID != p.mchID {
		return payment.FailResponse("application/json", wechatFailResponse, http.StatusBadRequest),
			fmt.Errorf("%w: 回调 mchid 与渠道配置不一致", payment.ErrInvalidSignature)
	}
	if tr.AppID != "" && tr.AppID != p.appID {
		return payment.FailResponse("application/json", wechatFailResponse, http.StatusBadRequest),
			fmt.Errorf("%w: 回调 appid 与渠道配置不一致", payment.ErrInvalidSignature)
	}

	success := env.EventType == "TRANSACTION.SUCCESS" && tr.TradeState == "SUCCESS"
	// 用 payer_total（用户实付）而非 total 做比对更严谨，
	// 但在无优惠的场景两者相同；取 payer_total 为准，缺失时回退 total。
	amount := tr.Amount.PayerTotal
	if amount == 0 {
		amount = tr.Amount.Total
	}

	return &payment.NotifyResult{
		Success:  success,
		OrderNo:  tr.OutTradeNo,
		TradeNo:  tr.TransactionID,
		Amount:   amount,
		Currency: tr.Amount.Currency,
		Status:   tr.TradeState,
		Raw: map[string]any{
			"event_type":   env.EventType,
			"out_trade_no": tr.OutTradeNo,
			"trade_state":  tr.TradeState,
			"total":        tr.Amount.Total,
			"payer_total":  tr.Amount.PayerTotal,
			"success_time": tr.SuccessTime,
		},
		ResponseBody:        `{"code":"SUCCESS","message":"成功"}`,
		ResponseContentType: "application/json; charset=utf-8",
		ResponseStatus:      http.StatusOK,
	}, nil
}

// QueryPayment 主动查询订单。
func (p *Provider) QueryPayment(ctx context.Context, req payment.QueryRequest) (*payment.PaymentStatus, error) {
	orderNo := req.OrderNo

	urlPath := fmt.Sprintf("/v3/pay/transactions/out-trade-no/%s?mchid=%s", orderNo, p.mchID)
	body, status, err := p.doGet(ctx, urlPath)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, wechatError(status, body)
	}
	var tr transactionResource
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("微信支付查询返回无法解析: %s", truncate(string(body), 300))
	}
	amount := tr.Amount.PayerTotal
	if amount == 0 {
		amount = tr.Amount.Total
	}
	return &payment.PaymentStatus{
		Paid:     tr.TradeState == "SUCCESS",
		TradeNo:  tr.TransactionID,
		Amount:   amount,
		Currency: tr.Amount.Currency,
		Status:   tr.TradeState,
		Raw:      truncate(string(body), 2000),
	}, nil
}

// Refund 发起退款。
func (p *Provider) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResult, error) {
	body := map[string]any{
		"out_trade_no":  req.OrderNo,
		"out_refund_no": req.RefundNo,
		"amount": map[string]any{
			"refund":   req.Amount,
			"total":    req.TotalAmount,
			"currency": "CNY",
		},
	}
	if req.TradeNo != "" {
		body["transaction_id"] = req.TradeNo
		delete(body, "out_trade_no")
	}
	if req.Reason != "" {
		body["reason"] = utils.TrimAndLimit(req.Reason, 80)
	}

	respBody, status, err := p.doPost(ctx, pathRefund, body)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, wechatError(status, respBody)
	}
	var out struct {
		RefundID string `json:"refund_id"`
		Status   string `json:"status"`
		Amount   struct {
			Refund int64 `json:"refund"`
		} `json:"amount"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("微信退款返回无法解析: %s", truncate(string(respBody), 300))
	}
	return &payment.RefundResult{
		// SUCCESS 立即到账；PROCESSING 表示受理成功但还在处理中，同样算发起成功
		Success:  out.Status == "SUCCESS" || out.Status == "PROCESSING",
		RefundNo: req.RefundNo,
		Amount:   out.Amount.Refund,
		Status:   out.Status,
		Raw:      truncate(string(respBody), 2000),
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
