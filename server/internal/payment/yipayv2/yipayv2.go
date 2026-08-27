// Package yipayv2 实现「易支付 V2」接口。
//
// V2 与 V1 是**两套独立协议**，不能假设参数一致，因此单独维护 Adapter：
//   - 接口地址改为 {gateway}/api/pay/create、/api/pay/query、/api/pay/refund
//   - 提交格式仍是 application/x-www-form-urlencoded，响应为 JSON
//   - 默认签名算法为 SHA256WithRSA（sign_type=RSA）：
//     用商户私钥签名，用平台公钥验签回调
//   - 同时保留 MD5 模式，兼容部分只开放 MD5 的 V2 站点
//   - 新增 timestamp 参数，服务端会校验时间窗口（抗重放）
package yipayv2

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/moecard/server/internal/payment"
	"github.com/moecard/server/internal/utils"
)

const (
	signRSA = "RSA"
	signMD5 = "MD5"
)

func init() {
	payment.Register(payment.Descriptor{
		Key:       payment.ProviderYipayV2,
		Name:      "易支付 V2",
		CanRefund: true,
		Note:      "易支付新版接口（SHA256WithRSA 签名，支持退款）。商户后台「API 信息」页可生成 RSA 密钥对。",
		Fields: []payment.ConfigField{
			{Key: "gateway", Label: "支付网关地址", Type: "text", Required: true,
				Placeholder: "https://pay.example.com", Help: "填写站点根地址，不要带 /api/pay/create"},
			{Key: "pid", Label: "商户 ID", Type: "text", Required: true},
			{Key: "sign_type", Label: "签名方式", Type: "select", Default: signRSA,
				Options: []payment.Option{
					{Label: "RSA（SHA256WithRSA，推荐）", Value: signRSA},
					{Label: "MD5（兼容模式）", Value: signMD5},
				}},
			{Key: "merchant_private_key", Label: "商户私钥", Type: "textarea", Secret: true,
				Help: "sign_type=RSA 时必填。商户后台生成的 RSA 密钥对中的私钥"},
			{Key: "platform_public_key", Label: "平台公钥", Type: "textarea", Secret: true,
				Help: "sign_type=RSA 时必填。用于验证异步通知签名"},
			{Key: "key", Label: "商户密钥 KEY", Type: "password", Secret: true,
				Help: "sign_type=MD5 时必填"},
			{Key: "pay_type", Label: "支付方式", Type: "select", Default: "alipay",
				Options: []payment.Option{
					{Label: "支付宝", Value: "alipay"},
					{Label: "微信支付", Value: "wxpay"},
					{Label: "QQ 钱包", Value: "qqpay"},
					{Label: "由收银台选择", Value: ""},
				}},
			{Key: "sitename", Label: "网站名称", Type: "text"},
		},
	}, New)
}

// Provider 是易支付 V2 的实现。
type Provider struct {
	gateway  string
	pid      string
	signType string
	key      string
	privPEM  string
	pubPEM   string
	payType  string
	sitename string
	client   *http.Client
}

// New 构造 Provider。
func New(cfg map[string]string) (payment.Provider, error) {
	gw := strings.TrimRight(strings.TrimSpace(cfg["gateway"]), "/")
	if gw == "" {
		return nil, fmt.Errorf("%w: 缺少 gateway", payment.ErrInvalidConfig)
	}
	if !strings.HasPrefix(gw, "http://") && !strings.HasPrefix(gw, "https://") {
		gw = "https://" + gw
	}

	st := strings.ToUpper(strings.TrimSpace(cfg["sign_type"]))
	if st == "" {
		st = signRSA
	}

	p := &Provider{
		gateway:  gw,
		pid:      strings.TrimSpace(cfg["pid"]),
		signType: st,
		key:      strings.TrimSpace(cfg["key"]),
		privPEM:  strings.TrimSpace(cfg["merchant_private_key"]),
		pubPEM:   strings.TrimSpace(cfg["platform_public_key"]),
		payType:  strings.TrimSpace(cfg["pay_type"]),
		sitename: strings.TrimSpace(cfg["sitename"]),
		client:   payment.NewHTTPClient(payment.DefaultTimeout),
	}

	// 启动时就把密钥解析一遍，配置错误立刻暴露，而不是等到用户下单才报错
	switch p.signType {
	case signRSA:
		if p.privPEM == "" || p.pubPEM == "" {
			return nil, fmt.Errorf("%w: RSA 模式需要同时配置商户私钥与平台公钥", payment.ErrInvalidConfig)
		}
		if _, err := payment.ParsePrivateKey(p.privPEM); err != nil {
			return nil, fmt.Errorf("商户私钥无效: %w", err)
		}
		if _, err := payment.ParsePublicKey(p.pubPEM); err != nil {
			return nil, fmt.Errorf("平台公钥无效: %w", err)
		}
	case signMD5:
		if p.key == "" {
			return nil, fmt.Errorf("%w: MD5 模式需要配置商户密钥 KEY", payment.ErrInvalidConfig)
		}
	default:
		return nil, fmt.Errorf("%w: 不支持的签名方式 %s", payment.ErrInvalidConfig, p.signType)
	}
	return p, nil
}

// Key 返回 provider 标识。
func (p *Provider) Key() string { return payment.ProviderYipayV2 }

// signContent 构造待签名串：ASCII 升序、剔除 sign/sign_type/空值、a=b&c=d、值不做 URL 编码。
func signContent(params map[string]string) string {
	return payment.SortedQuery(params, "sign", "sign_type")
}

// sign 按配置的算法生成签名。
func (p *Provider) sign(params map[string]string) (string, error) {
	content := signContent(params)
	if p.signType == signMD5 {
		return payment.MD5Hex(content + p.key), nil
	}
	key, err := payment.ParsePrivateKey(p.privPEM)
	if err != nil {
		return "", err
	}
	return payment.SignRSA(key, content, "SHA256")
}

// verify 校验签名。
func (p *Provider) verify(params map[string]string, signature string) error {
	content := signContent(params)
	if p.signType == signMD5 {
		if !payment.SecureCompareHex(signature, payment.MD5Hex(content+p.key)) {
			return fmt.Errorf("%w: MD5 签名不匹配", payment.ErrInvalidSignature)
		}
		return nil
	}
	pub, err := payment.ParsePublicKey(p.pubPEM)
	if err != nil {
		return err
	}
	return payment.VerifyRSA(pub, content, signature, "SHA256")
}

// CreatePayment 发起支付。
func (p *Provider) CreatePayment(ctx context.Context, req payment.PaymentRequest) (*payment.PaymentResponse, error) {
	device := "pc"
	if req.Device == "mobile" {
		device = "mobile"
	}
	params := map[string]string{
		"pid":          p.pid,
		"method":       "web",
		"device":       device,
		"out_trade_no": req.OrderNo,
		"notify_url":   req.NotifyURL,
		"return_url":   req.ReturnURL,
		"name":         sanitizeName(req.Subject),
		"money":        utils.AmountToYuanString(req.Amount),
		"timestamp":    payment.TimestampSec(),
	}
	if p.payType != "" {
		params["type"] = p.payType
	}
	if p.sitename != "" {
		params["sitename"] = p.sitename
	}
	if req.ClientIP != "" {
		params["clientip"] = req.ClientIP
	}

	sig, err := p.sign(params)
	if err != nil {
		return nil, err
	}
	params["sign"] = sig
	params["sign_type"] = p.signType

	res, err := payment.PostForm(ctx, p.client, p.gateway+"/api/pay/create", params)
	if err != nil {
		return nil, err
	}

	var out struct {
		Code      any    `json:"code"`
		Msg       string `json:"msg"`
		TradeNo   string `json:"trade_no"`
		PayURL    string `json:"payurl"`
		QRCode    string `json:"qrcode"`
		URLScheme string `json:"urlscheme"`
		PayType   string `json:"pay_type"`
	}
	if err := json.Unmarshal(res.Body, &out); err != nil {
		return nil, fmt.Errorf("易支付V2 返回内容无法解析(HTTP %d): %s", res.StatusCode, truncate(string(res.Body), 300))
	}
	if fmt.Sprint(out.Code) != "1" {
		msg := out.Msg
		if msg == "" {
			msg = "未知错误"
		}
		return nil, fmt.Errorf("易支付V2 下单失败: %s", msg)
	}

	resp := &payment.PaymentResponse{TradeNo: out.TradeNo, Raw: truncate(string(res.Body), 2000)}
	switch {
	case out.PayURL != "":
		resp.Action, resp.URL = payment.ActionRedirect, out.PayURL
	case out.QRCode != "":
		resp.Action, resp.QRCode = payment.ActionQRCode, out.QRCode
	case out.URLScheme != "":
		resp.Action, resp.URL = payment.ActionRedirect, out.URLScheme
	default:
		return nil, fmt.Errorf("易支付V2 未返回可用的支付地址")
	}
	return resp, nil
}

// VerifyNotify 校验异步通知。
func (p *Provider) VerifyNotify(ctx context.Context, req payment.NotifyRequest) (*payment.NotifyResult, error) {
	raw := payment.ValuesToMap(req.Form)
	if len(raw) == 0 {
		raw = payment.ValuesToMap(req.Query)
	}
	if len(raw) == 0 && len(req.Body) > 0 {
		// 部分站点用 JSON 回调
		var m map[string]any
		if err := json.Unmarshal(req.Body, &m); err == nil {
			raw = make(map[string]string, len(m))
			for k, v := range m {
				raw[k] = fmt.Sprint(v)
			}
		}
	}
	if len(raw) == 0 {
		return payment.FailResponse("text/plain", "fail", http.StatusBadRequest),
			fmt.Errorf("%w: 回调参数为空", payment.ErrInvalidSignature)
	}

	if err := p.verify(raw, raw["sign"]); err != nil {
		return payment.FailResponse("text/plain", "fail", http.StatusBadRequest), err
	}
	if raw["pid"] != "" && raw["pid"] != p.pid {
		return payment.FailResponse("text/plain", "fail", http.StatusBadRequest),
			fmt.Errorf("%w: 回调 pid 与渠道配置不一致", payment.ErrInvalidSignature)
	}

	amount, err := utils.ParseAmount(raw["money"])
	if err != nil {
		return payment.FailResponse("text/plain", "fail", http.StatusBadRequest),
			fmt.Errorf("回调金额格式错误: %s", raw["money"])
	}

	return &payment.NotifyResult{
		Success:             strings.EqualFold(raw["trade_status"], "TRADE_SUCCESS"),
		OrderNo:             raw["out_trade_no"],
		TradeNo:             raw["trade_no"],
		Amount:              amount,
		Currency:            "CNY",
		Status:              raw["trade_status"],
		Raw:                 toAnyMap(utils.SanitizeMap(raw)),
		ResponseBody:        "success",
		ResponseContentType: "text/plain; charset=utf-8",
		ResponseStatus:      http.StatusOK,
	}, nil
}

// QueryPayment 主动查询订单。
func (p *Provider) QueryPayment(ctx context.Context, req payment.QueryRequest) (*payment.PaymentStatus, error) {
	orderNo := req.OrderNo

	params := map[string]string{
		"pid":          p.pid,
		"out_trade_no": orderNo,
		"timestamp":    payment.TimestampSec(),
	}
	sig, err := p.sign(params)
	if err != nil {
		return nil, err
	}
	params["sign"] = sig
	params["sign_type"] = p.signType

	res, err := payment.PostForm(ctx, p.client, p.gateway+"/api/pay/query", params)
	if err != nil {
		return nil, err
	}
	var out struct {
		Code    any    `json:"code"`
		Msg     string `json:"msg"`
		TradeNo string `json:"trade_no"`
		Status  any    `json:"status"`
		Money   string `json:"money"`
	}
	if err := json.Unmarshal(res.Body, &out); err != nil {
		return nil, fmt.Errorf("易支付V2 查询返回无法解析: %s", truncate(string(res.Body), 300))
	}
	if fmt.Sprint(out.Code) != "1" {
		return nil, fmt.Errorf("易支付V2 查询失败: %s", out.Msg)
	}
	amount, _ := utils.ParseAmount(out.Money)
	statusStr := fmt.Sprint(out.Status)
	return &payment.PaymentStatus{
		Paid:     statusStr == "1",
		TradeNo:  out.TradeNo,
		Amount:   amount,
		Currency: "CNY",
		Status:   statusStr,
		Raw:      truncate(string(res.Body), 2000),
	}, nil
}

// Refund 发起退款。
func (p *Provider) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResult, error) {
	if p.signType != signRSA {
		// V2 的退款接口要求 RSA 签名；MD5 模式不具备该能力
		return nil, payment.ErrNotSupported
	}
	params := map[string]string{
		"pid":          p.pid,
		"out_trade_no": req.OrderNo,
		"money":        utils.AmountToYuanString(req.Amount),
		"timestamp":    payment.TimestampSec(),
	}
	if req.TradeNo != "" {
		params["trade_no"] = req.TradeNo
	}
	sig, err := p.sign(params)
	if err != nil {
		return nil, err
	}
	params["sign"] = sig
	params["sign_type"] = p.signType

	res, err := payment.PostForm(ctx, p.client, p.gateway+"/api/pay/refund", params)
	if err != nil {
		return nil, err
	}
	var out struct {
		Code any    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(res.Body, &out); err != nil {
		return nil, fmt.Errorf("易支付V2 退款返回无法解析: %s", truncate(string(res.Body), 300))
	}
	ok := fmt.Sprint(out.Code) == "1"
	if !ok {
		return nil, fmt.Errorf("易支付V2 退款失败: %s", out.Msg)
	}
	return &payment.RefundResult{
		Success:  true,
		RefundNo: req.RefundNo,
		Amount:   req.Amount,
		Status:   "success",
		Raw:      truncate(string(res.Body), 2000),
	}, nil
}

func sanitizeName(s string) string {
	s = strings.NewReplacer("&", " ", "=", " ", "\n", " ", "\r", " ").Replace(s)
	return utils.TrimAndLimit(s, 100)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func toAnyMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
