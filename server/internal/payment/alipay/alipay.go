// Package alipay 实现支付宝开放平台官方接口。
//
// 协议要点：
//   - 网关 https://openapi.alipay.com/gateway.do
//   - 公共参数：app_id / method / format=JSON / charset=utf-8 / sign_type=RSA2 /
//     timestamp（GMT+8 的 yyyy-MM-dd HH:mm:ss）/ version=1.0 / biz_content / sign
//   - 签名：公共参数按 ASCII 升序、剔除 sign 与空值、拼成 a=b&c=d，
//     用应用私钥做 SHA256withRSA，结果 base64
//   - 验签：用支付宝公钥验证，待验串同样是"剔除 sign/sign_type 后按 ASCII 升序拼接"
//   - 异步通知必须应答纯文本 success
//
// 结构上已为「当面付 / 电脑网站 / 手机网站」预留：method 与 product_code
// 由 pay_method 配置驱动，新增一种只需加一个枚举值。
package alipay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/moecard/server/internal/payment"
	"github.com/moecard/server/internal/utils"
)

const (
	prodGateway    = "https://openapi.alipay.com/gateway.do"
	sandboxGateway = "https://openapi-sandbox.dl.alipaydev.com/gateway.do"

	methodPagePay  = "alipay.trade.page.pay"  // 电脑网站支付
	methodWapPay   = "alipay.trade.wap.pay"   // 手机网站支付
	methodFaceToF  = "alipay.trade.precreate" // 当面付（扫码）
	methodQuery    = "alipay.trade.query"
	methodRefund   = "alipay.trade.refund"
	methodCloseOrd = "alipay.trade.close"
)

func init() {
	payment.Register(payment.Descriptor{
		Key:       payment.ProviderAlipay,
		Name:      "支付宝（官方）",
		CanRefund: true,
		Note:      "支付宝开放平台官方接口，RSA2 签名。需在开放平台配置应用公钥并获取支付宝公钥。",
		Fields: []payment.ConfigField{
			{Key: "app_id", Label: "AppID", Type: "text", Required: true},
			{Key: "app_private_key", Label: "应用私钥", Type: "textarea", Required: true, Secret: true,
				Help: "支付宝开放平台助手生成的应用私钥（PKCS#1 或 PKCS#8 均可）"},
			{Key: "alipay_public_key", Label: "支付宝公钥", Type: "textarea", Required: true, Secret: true,
				Help: "从开放平台「接口加签方式」处复制的支付宝公钥；也可直接粘贴支付宝公钥证书"},
			{Key: "pay_method", Label: "支付方式", Type: "select", Default: "auto",
				Options: []payment.Option{
					{Label: "自动（PC 用电脑网站，手机用手机网站）", Value: "auto"},
					{Label: "电脑网站支付", Value: "page"},
					{Label: "手机网站支付", Value: "wap"},
					{Label: "当面付（扫码）", Value: "face"},
				}},
			{Key: "sandbox", Label: "沙箱环境", Type: "switch", Default: "0"},
			{Key: "sign_type", Label: "签名算法", Type: "select", Default: "RSA2",
				Options: []payment.Option{
					{Label: "RSA2（SHA256，推荐）", Value: "RSA2"},
					{Label: "RSA（SHA1，已不推荐）", Value: "RSA"},
				}},
		},
	}, New)
}

// Provider 是支付宝的实现。
type Provider struct {
	appID     string
	privPEM   string
	pubPEM    string
	payMethod string
	signType  string
	gateway   string
	client    *http.Client
}

// New 构造 Provider。
func New(cfg map[string]string) (payment.Provider, error) {
	p := &Provider{
		appID:     strings.TrimSpace(cfg["app_id"]),
		privPEM:   strings.TrimSpace(cfg["app_private_key"]),
		pubPEM:    strings.TrimSpace(cfg["alipay_public_key"]),
		payMethod: strings.TrimSpace(cfg["pay_method"]),
		signType:  strings.ToUpper(strings.TrimSpace(cfg["sign_type"])),
		gateway:   prodGateway,
		client:    payment.NewHTTPClient(payment.DefaultTimeout),
	}
	if p.payMethod == "" {
		p.payMethod = "auto"
	}
	if p.signType != "RSA" {
		p.signType = "RSA2"
	}
	if cfg["sandbox"] == "1" || strings.EqualFold(cfg["sandbox"], "true") {
		p.gateway = sandboxGateway
	}

	// 启动即校验密钥，避免配置错误拖到用户下单时才爆
	if _, err := payment.ParsePrivateKey(p.privPEM); err != nil {
		return nil, fmt.Errorf("应用私钥无效: %w", err)
	}
	if _, err := payment.ParsePublicKey(p.pubPEM); err != nil {
		return nil, fmt.Errorf("支付宝公钥无效: %w", err)
	}
	return p, nil
}

// Key 返回 provider 标识。
func (p *Provider) Key() string { return payment.ProviderAlipay }

func (p *Provider) hashAlg() string {
	if p.signType == "RSA" {
		return "SHA1"
	}
	return "SHA256"
}

// commonParams 构造支付宝公共请求参数。
func (p *Provider) commonParams(method, bizContent string) map[string]string {
	return map[string]string{
		"app_id":  p.appID,
		"method":  method,
		"format":  "JSON",
		"charset": "utf-8",
		// 支付宝要求 timestamp 使用北京时间；我们内部统一 UTC，这里显式转换。
		// 这是"展示/协议边界才做时区转换"原则的体现，业务层依旧无需感知时区。
		"timestamp":   time.Now().In(utils.LoadLocation("Asia/Shanghai")).Format("2006-01-02 15:04:05"),
		"version":     "1.0",
		"sign_type":   p.signType,
		"biz_content": bizContent,
	}
}

func (p *Provider) signParams(params map[string]string) error {
	key, err := payment.ParsePrivateKey(p.privPEM)
	if err != nil {
		return err
	}
	sig, err := payment.SignRSA(key, payment.SortedQuery(params, "sign"), p.hashAlg())
	if err != nil {
		return err
	}
	params["sign"] = sig
	return nil
}

// resolveMethod 决定使用哪个支付接口。
func (p *Provider) resolveMethod(device string) (apiMethod, productCode string) {
	switch p.payMethod {
	case "page":
		return methodPagePay, "FAST_INSTANT_TRADE_PAY"
	case "wap":
		return methodWapPay, "QUICK_WAP_WAY"
	case "face":
		return methodFaceToF, "FACE_TO_FACE_PAYMENT"
	default: // auto
		if device == "mobile" {
			return methodWapPay, "QUICK_WAP_WAY"
		}
		return methodPagePay, "FAST_INSTANT_TRADE_PAY"
	}
}

// CreatePayment 发起支付。
func (p *Provider) CreatePayment(ctx context.Context, req payment.PaymentRequest) (*payment.PaymentResponse, error) {
	apiMethod, productCode := p.resolveMethod(req.Device)

	biz := map[string]any{
		"out_trade_no": req.OrderNo,
		"total_amount": utils.AmountToYuanString(req.Amount),
		"subject":      utils.TrimAndLimit(req.Subject, 200),
		"product_code": productCode,
	}
	if req.Body != "" {
		biz["body"] = utils.TrimAndLimit(utils.StripHTML(req.Body), 120)
	}
	if apiMethod == methodPagePay {
		// 电脑网站支付支持指定超时，超时后支付宝会自动关单
		biz["qr_pay_mode"] = "0"
	}
	bizJSON, err := json.Marshal(biz)
	if err != nil {
		return nil, fmt.Errorf("构造 biz_content 失败: %w", err)
	}

	params := p.commonParams(apiMethod, string(bizJSON))
	if req.NotifyURL != "" {
		params["notify_url"] = req.NotifyURL
	}
	if req.ReturnURL != "" && apiMethod != methodFaceToF {
		params["return_url"] = req.ReturnURL
	}
	if err := p.signParams(params); err != nil {
		return nil, err
	}

	// 当面付走服务端调用，拿二维码；网页支付走浏览器表单跳转
	if apiMethod == methodFaceToF {
		node, err := p.call(ctx, params, "alipay_trade_precreate_response")
		if err != nil {
			return nil, err
		}
		qr, _ := node["qr_code"].(string)
		if qr == "" {
			return nil, fmt.Errorf("支付宝未返回二维码")
		}
		return &payment.PaymentResponse{Action: payment.ActionQRCode, QRCode: qr}, nil
	}

	return &payment.PaymentResponse{
		Action:   payment.ActionForm,
		FormHTML: payment.BuildAutoSubmitForm(p.gateway+"?charset=utf-8", params, http.MethodPost),
	}, nil
}

// VerifyNotify 校验异步通知。
//
// 关键点：
//  1. 待验签串是**支付宝原样发来的参数**，剔除 sign / sign_type 后按 ASCII 升序拼接。
//     必须用解析后的值（而非原始 URL 编码串）参与验签。
//  2. 必须校验 app_id —— 否则任何人拿自己的支付宝应用给我们发通知都会被认作有效。
//  3. trade_status 只有 TRADE_SUCCESS / TRADE_FINISHED 才算收到钱。
func (p *Provider) VerifyNotify(ctx context.Context, req payment.NotifyRequest) (*payment.NotifyResult, error) {
	raw := payment.ValuesToMap(req.Form)
	if len(raw) == 0 {
		raw = payment.ValuesToMap(req.Query)
	}
	if len(raw) == 0 {
		return payment.FailResponse("text/plain", "fail", http.StatusBadRequest),
			fmt.Errorf("%w: 支付宝回调参数为空", payment.ErrInvalidSignature)
	}

	pub, err := payment.ParsePublicKey(p.pubPEM)
	if err != nil {
		return payment.FailResponse("text/plain", "fail", http.StatusInternalServerError), err
	}
	content := payment.SortedQuery(raw, "sign", "sign_type")
	alg := "SHA256"
	if strings.EqualFold(raw["sign_type"], "RSA") {
		alg = "SHA1"
	}
	if err := payment.VerifyRSA(pub, content, raw["sign"], alg); err != nil {
		return payment.FailResponse("text/plain", "fail", http.StatusBadRequest),
			fmt.Errorf("支付宝回调验签失败: %w", err)
	}
	if raw["app_id"] != "" && raw["app_id"] != p.appID {
		return payment.FailResponse("text/plain", "fail", http.StatusBadRequest),
			fmt.Errorf("%w: 回调 app_id 与渠道配置不一致", payment.ErrInvalidSignature)
	}

	amount, err := utils.ParseAmount(raw["total_amount"])
	if err != nil {
		return payment.FailResponse("text/plain", "fail", http.StatusBadRequest),
			fmt.Errorf("回调金额格式错误: %s", raw["total_amount"])
	}

	status := raw["trade_status"]
	return &payment.NotifyResult{
		Success:             status == "TRADE_SUCCESS" || status == "TRADE_FINISHED",
		OrderNo:             raw["out_trade_no"],
		TradeNo:             raw["trade_no"],
		Amount:              amount,
		Currency:            "CNY",
		Status:              status,
		Raw:                 toAnyMap(utils.SanitizeMap(raw)),
		ResponseBody:        "success",
		ResponseContentType: "text/plain; charset=utf-8",
		ResponseStatus:      http.StatusOK,
	}, nil
}

// QueryPayment 主动查询订单。
func (p *Provider) QueryPayment(ctx context.Context, req payment.QueryRequest) (*payment.PaymentStatus, error) {
	orderNo := req.OrderNo

	bizJSON, _ := json.Marshal(map[string]any{"out_trade_no": orderNo})
	params := p.commonParams(methodQuery, string(bizJSON))
	if err := p.signParams(params); err != nil {
		return nil, err
	}
	node, err := p.call(ctx, params, "alipay_trade_query_response")
	if err != nil {
		return nil, err
	}

	status, _ := node["trade_status"].(string)
	tradeNo, _ := node["trade_no"].(string)
	totalStr, _ := node["total_amount"].(string)
	amount, _ := utils.ParseAmount(totalStr)

	rawJSON, _ := json.Marshal(node)
	return &payment.PaymentStatus{
		Paid:     status == "TRADE_SUCCESS" || status == "TRADE_FINISHED",
		TradeNo:  tradeNo,
		Amount:   amount,
		Currency: "CNY",
		Status:   status,
		Raw:      truncate(string(rawJSON), 2000),
	}, nil
}

// Refund 发起退款。
func (p *Provider) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResult, error) {
	biz := map[string]any{
		"out_trade_no":  req.OrderNo,
		"refund_amount": utils.AmountToYuanString(req.Amount),
	}
	if req.RefundNo != "" {
		biz["out_request_no"] = req.RefundNo
	}
	if req.Reason != "" {
		biz["refund_reason"] = utils.TrimAndLimit(req.Reason, 200)
	}
	bizJSON, _ := json.Marshal(biz)

	params := p.commonParams(methodRefund, string(bizJSON))
	if err := p.signParams(params); err != nil {
		return nil, err
	}
	node, err := p.call(ctx, params, "alipay_trade_refund_response")
	if err != nil {
		return nil, err
	}

	feeStr, _ := node["refund_fee"].(string)
	amount, _ := utils.ParseAmount(feeStr)
	if amount == 0 {
		amount = req.Amount
	}
	rawJSON, _ := json.Marshal(node)
	return &payment.RefundResult{
		Success:  true,
		RefundNo: req.RefundNo,
		Amount:   amount,
		Status:   "success",
		Raw:      truncate(string(rawJSON), 2000),
	}, nil
}

// CloseOrder 关闭未支付订单（订单过期时调用，避免用户在支付宝侧继续付款）。
func (p *Provider) CloseOrder(ctx context.Context, orderNo string) error {
	bizJSON, _ := json.Marshal(map[string]any{"out_trade_no": orderNo})
	params := p.commonParams(methodCloseOrd, string(bizJSON))
	if err := p.signParams(params); err != nil {
		return err
	}
	_, err := p.call(ctx, params, "alipay_trade_close_response")
	return err
}

// call 调用支付宝网关并解析 {xxx_response: {...}} 结构。
func (p *Provider) call(ctx context.Context, params map[string]string, nodeKey string) (map[string]any, error) {
	res, err := payment.PostForm(ctx, p.client, p.gateway, params)
	if err != nil {
		return nil, err
	}

	// 支付宝返回的是 GBK 还是 UTF-8 取决于 charset 参数，我们固定用 utf-8
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(res.Body, &envelope); err != nil {
		return nil, fmt.Errorf("支付宝返回内容无法解析(HTTP %d): %s", res.StatusCode, truncate(string(res.Body), 300))
	}
	rawNode, ok := envelope[nodeKey]
	if !ok {
		// 出错时支付宝返回 error_response
		if errNode, ok := envelope["error_response"]; ok {
			var e struct {
				Code    string `json:"code"`
				Msg     string `json:"msg"`
				SubMsg  string `json:"sub_msg"`
				SubCode string `json:"sub_code"`
			}
			_ = json.Unmarshal(errNode, &e)
			return nil, fmt.Errorf("支付宝接口错误 [%s/%s] %s %s", e.Code, e.SubCode, e.Msg, e.SubMsg)
		}
		return nil, fmt.Errorf("支付宝返回缺少 %s 节点: %s", nodeKey, truncate(string(res.Body), 300))
	}

	var node map[string]any
	if err := json.Unmarshal(rawNode, &node); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", nodeKey, err)
	}
	code, _ := node["code"].(string)
	if code != "10000" {
		msg, _ := node["msg"].(string)
		subMsg, _ := node["sub_msg"].(string)
		return nil, fmt.Errorf("支付宝接口失败 [%s] %s %s", code, msg, subMsg)
	}
	return node, nil
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
