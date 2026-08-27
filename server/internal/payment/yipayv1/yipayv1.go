// Package yipayv1 实现「易支付 V1」（彩虹易支付经典接口）。
//
// 协议要点（来自彩虹易支付官方文档）：
//   - 提交格式 application/x-www-form-urlencoded
//   - 签名算法 MD5：参数按 ASCII 升序、剔除 sign/sign_type/空值，
//     拼成 a=b&c=d 后直接拼接商户 KEY，取 md5 小写
//   - 页面跳转下单：{gateway}/submit.php
//   - API 下单：    {gateway}/mapi.php（返回 JSON）
//   - 订单查询：    {gateway}/api.php?act=order
//   - 异步通知：GET，携带 trade_status=TRADE_SUCCESS，商户需应答纯文本 success
//   - 金额单位为**元**，两位小数
package yipayv1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/moecard/server/internal/payment"
	"github.com/moecard/server/internal/utils"
)

func init() {
	payment.Register(payment.Descriptor{
		Key:       payment.ProviderYipayV1,
		Name:      "易支付 V1",
		CanRefund: false,
		Note:      "彩虹易支付经典接口（MD5 签名）。适用于绝大多数第三方易支付站点。",
		Fields: []payment.ConfigField{
			{Key: "gateway", Label: "支付网关地址", Type: "text", Required: true,
				Placeholder: "https://pay.example.com", Help: "填写易支付站点根地址，不要带 /submit.php"},
			{Key: "pid", Label: "商户 PID", Type: "text", Required: true},
			{Key: "key", Label: "商户 KEY", Type: "password", Required: true, Secret: true},
			{Key: "pay_type", Label: "支付方式", Type: "select", Default: "alipay",
				Options: []payment.Option{
					{Label: "支付宝", Value: "alipay"},
					{Label: "微信支付", Value: "wxpay"},
					{Label: "QQ 钱包", Value: "qqpay"},
					{Label: "财付通", Value: "tenpay"},
					{Label: "由收银台选择", Value: ""},
				}},
			{Key: "mode", Label: "下单方式", Type: "select", Default: "page",
				Options: []payment.Option{
					{Label: "页面跳转（submit.php）", Value: "page"},
					{Label: "API 下单（mapi.php）", Value: "api"},
				},
				Help: "API 下单可直接拿到二维码/支付链接；页面跳转兼容性最好"},
			{Key: "sitename", Label: "网站名称", Type: "text", Placeholder: "可选，展示在收银台"},
		},
	}, New)
}

// Provider 是易支付 V1 的实现。
type Provider struct {
	gateway  string
	pid      string
	key      string
	payType  string
	mode     string
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
	mode := cfg["mode"]
	if mode == "" {
		mode = "page"
	}
	return &Provider{
		gateway:  gw,
		pid:      strings.TrimSpace(cfg["pid"]),
		key:      strings.TrimSpace(cfg["key"]),
		payType:  strings.TrimSpace(cfg["pay_type"]),
		mode:     mode,
		sitename: strings.TrimSpace(cfg["sitename"]),
		client:   payment.NewHTTPClient(payment.DefaultTimeout),
	}, nil
}

// Key 返回 provider 标识。
func (p *Provider) Key() string { return payment.ProviderYipayV1 }

// sign 计算 MD5 签名。
func (p *Provider) sign(params map[string]string) string {
	return payment.MD5Hex(payment.SortedQuery(params, "sign", "sign_type") + p.key)
}

// buildParams 组装下单参数。
func (p *Provider) buildParams(req payment.PaymentRequest) map[string]string {
	params := map[string]string{
		"pid":          p.pid,
		"out_trade_no": req.OrderNo,
		"notify_url":   req.NotifyURL,
		"return_url":   req.ReturnURL,
		// 商品名里的 & 和 = 会破坏签名串的结构，必须替换掉
		"name":  sanitizeName(req.Subject),
		"money": utils.AmountToYuanString(req.Amount),
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
	return params
}

// CreatePayment 发起支付。
func (p *Provider) CreatePayment(ctx context.Context, req payment.PaymentRequest) (*payment.PaymentResponse, error) {
	params := p.buildParams(req)
	params["sign"] = p.sign(params)
	params["sign_type"] = "MD5"

	if p.mode != "api" {
		// 页面跳转：生成自动提交表单，交给浏览器 POST 到 submit.php
		return &payment.PaymentResponse{
			Action:   payment.ActionForm,
			FormHTML: payment.BuildAutoSubmitForm(p.gateway+"/submit.php", params, http.MethodPost),
			Raw:      payment.SortedQuery(params, "sign", "key"),
		}, nil
	}

	res, err := payment.PostForm(ctx, p.client, p.gateway+"/mapi.php", params)
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
	}
	if err := json.Unmarshal(res.Body, &out); err != nil {
		return nil, fmt.Errorf("易支付返回内容无法解析(HTTP %d): %s", res.StatusCode, truncate(string(res.Body), 300))
	}
	if fmt.Sprint(out.Code) != "1" {
		msg := out.Msg
		if msg == "" {
			msg = "未知错误"
		}
		return nil, fmt.Errorf("易支付下单失败: %s", msg)
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
		return nil, fmt.Errorf("易支付未返回可用的支付地址")
	}
	return resp, nil
}

// VerifyNotify 校验异步通知。
//
// 易支付用 GET 回调，但部分站点会用 POST，这里两种都兼容。
func (p *Provider) VerifyNotify(ctx context.Context, req payment.NotifyRequest) (*payment.NotifyResult, error) {
	raw := payment.ValuesToMap(req.Query)
	if len(raw) == 0 && len(req.Form) > 0 {
		raw = payment.ValuesToMap(req.Form)
	}
	if len(raw) == 0 {
		return payment.FailResponse("text/plain", "fail", http.StatusBadRequest),
			fmt.Errorf("%w: 回调参数为空", payment.ErrInvalidSignature)
	}

	got := raw["sign"]
	want := p.sign(raw)
	if !payment.SecureCompareHex(got, want) {
		return payment.FailResponse("text/plain", "fail", http.StatusBadRequest),
			fmt.Errorf("%w: 易支付回调签名不匹配", payment.ErrInvalidSignature)
	}
	// 商户号必须匹配，否则别人用自己的易支付账号也能给我们发"成功"通知
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

	res, err := payment.GetURL(ctx, p.client, p.gateway+"/api.php", map[string]string{
		"act":          "order",
		"pid":          p.pid,
		"key":          p.key,
		"out_trade_no": orderNo,
	}, nil)
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
		return nil, fmt.Errorf("易支付查询返回无法解析: %s", truncate(string(res.Body), 300))
	}
	if fmt.Sprint(out.Code) != "1" {
		return nil, fmt.Errorf("易支付查询失败: %s", out.Msg)
	}
	amount, _ := utils.ParseAmount(out.Money)
	statusStr := fmt.Sprint(out.Status)
	return &payment.PaymentStatus{
		Paid:     statusStr == "1",
		TradeNo:  out.TradeNo,
		Amount:   amount,
		Currency: "CNY",
		Status:   statusStr,
		// 原始响应含 key，绝不能原样落库
		Raw: truncate(strings.ReplaceAll(string(res.Body), p.key, "***"), 2000),
	}, nil
}

// Refund 易支付 V1 无统一退款接口，返回不支持，由业务层降级为人工退款。
func (p *Provider) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResult, error) {
	return nil, payment.ErrNotSupported
}

// sanitizeName 清理商品名中会破坏签名串结构的字符。
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
