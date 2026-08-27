package yipayv1

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/moecard/server/internal/payment"
)

const (
	testPID = "1001"
	testKey = "abcdef0123456789"
)

func newTestProvider(t *testing.T) *Provider {
	t.Helper()
	p, err := New(map[string]string{
		"gateway": "https://pay.example.com",
		"pid":     testPID,
		"key":     testKey,
	})
	if err != nil {
		t.Fatalf("构造 provider 失败: %v", err)
	}
	return p.(*Provider)
}

// signedNotify 构造一条签名正确的回调。
func signedNotify(p *Provider, override map[string]string) url.Values {
	params := map[string]string{
		"pid":          testPID,
		"trade_no":     "2026082622001",
		"out_trade_no": "20260826123456ABCD",
		"type":         "alipay",
		"name":         "VIP",
		"money":        "10.00",
		"trade_status": "TRADE_SUCCESS",
	}
	for k, v := range override {
		if v == "" {
			delete(params, k)
			continue
		}
		params[k] = v
	}
	params["sign"] = p.sign(params)
	params["sign_type"] = "MD5"
	return payment.MapToValues(params)
}

// TestVerifyNotify_Valid 验证正确签名的回调被接受。
func TestVerifyNotify_Valid(t *testing.T) {
	p := newTestProvider(t)
	q := signedNotify(p, nil)

	res, err := p.VerifyNotify(context.Background(), payment.NotifyRequest{
		Method: http.MethodGet,
		Header: http.Header{},
		Query:  q,
	})
	if err != nil {
		t.Fatalf("合法回调不应报错: %v", err)
	}
	if !res.Success {
		t.Error("trade_status=TRADE_SUCCESS 应当被判定为支付成功")
	}
	if res.OrderNo != "20260826123456ABCD" {
		t.Errorf("订单号解析错误: %q", res.OrderNo)
	}
	if res.TradeNo != "2026082622001" {
		t.Errorf("平台交易号解析错误: %q", res.TradeNo)
	}
	// 金额必须换算成分
	if res.Amount != 1000 {
		t.Errorf("金额应换算为 1000 分，实际 %d", res.Amount)
	}
	if res.ResponseBody != "success" {
		t.Errorf("易支付要求应答 success，实际 %q", res.ResponseBody)
	}
	// 敏感字段不应原样出现在落库内容里
	if sign, ok := res.Raw["sign"].(string); ok && sign == q.Get("sign") {
		t.Error("落库的原始报文中 sign 应当被脱敏")
	}
}

// TestVerifyNotify_TamperedSignature 验证篡改签名的回调被拒绝。
//
// 这是最关键的安全测试：如果这个测试挂了，任何人都能伪造支付成功通知白嫖商品。
func TestVerifyNotify_TamperedSignature(t *testing.T) {
	p := newTestProvider(t)

	cases := []struct {
		name   string
		mutate func(url.Values)
	}{
		{"直接改签名", func(q url.Values) { q.Set("sign", "00000000000000000000000000000000") }},
		{"删除签名", func(q url.Values) { q.Del("sign") }},
		{"篡改金额", func(q url.Values) { q.Set("money", "0.01") }},
		{"篡改订单号", func(q url.Values) { q.Set("out_trade_no", "OTHER-ORDER") }},
		{"篡改商户号", func(q url.Values) { q.Set("pid", "9999") }},
		{"新增参数", func(q url.Values) { q.Set("evil", "1") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := signedNotify(p, nil)
			tc.mutate(q)

			res, err := p.VerifyNotify(context.Background(), payment.NotifyRequest{
				Method: http.MethodGet, Header: http.Header{}, Query: q,
			})
			if err == nil {
				t.Fatal("被篡改的回调竟然通过了验签！")
			}
			if res != nil && res.Success {
				t.Fatal("被篡改的回调不应返回 Success=true")
			}
		})
	}
}

// TestVerifyNotify_WrongMerchant 验证商户号不匹配时被拒绝。
//
// 场景：攻击者用自己的易支付账号，把 notify_url 指向我们的商城。
// 签名对他自己的 pid+key 是有效的，但 pid 与我们的渠道配置不符，必须拒绝。
func TestVerifyNotify_WrongMerchant(t *testing.T) {
	p := newTestProvider(t)

	// 攻击者的商户号与密钥
	attacker, _ := New(map[string]string{
		"gateway": "https://pay.example.com", "pid": "6666", "key": "attacker-key",
	})
	q := signedNotify(attacker.(*Provider), map[string]string{"pid": "6666"})

	if _, err := p.VerifyNotify(context.Background(), payment.NotifyRequest{
		Method: http.MethodGet, Header: http.Header{}, Query: q,
	}); err == nil {
		t.Fatal("其他商户签名的回调竟然通过了！")
	}
}

// TestVerifyNotify_NotSuccess 验证非成功状态不触发发货。
func TestVerifyNotify_NotSuccess(t *testing.T) {
	p := newTestProvider(t)
	q := signedNotify(p, map[string]string{"trade_status": "WAIT_BUYER_PAY"})

	res, err := p.VerifyNotify(context.Background(), payment.NotifyRequest{
		Method: http.MethodGet, Header: http.Header{}, Query: q,
	})
	if err != nil {
		t.Fatalf("签名有效的回调不应报错: %v", err)
	}
	if res.Success {
		t.Error("trade_status 不是 TRADE_SUCCESS 时不应判定为支付成功")
	}
}

// TestVerifyNotify_EmptyPayload 验证空回调被拒绝。
func TestVerifyNotify_EmptyPayload(t *testing.T) {
	p := newTestProvider(t)
	if _, err := p.VerifyNotify(context.Background(), payment.NotifyRequest{
		Method: http.MethodPost, Header: http.Header{},
	}); err == nil {
		t.Fatal("空回调应当被拒绝")
	}
}

// TestVerifyNotify_POSTForm 验证部分站点用 POST 回调时也能处理。
func TestVerifyNotify_POSTForm(t *testing.T) {
	p := newTestProvider(t)
	form := signedNotify(p, nil)

	res, err := p.VerifyNotify(context.Background(), payment.NotifyRequest{
		Method:      http.MethodPost,
		Header:      http.Header{},
		Form:        form,
		ContentType: "application/x-www-form-urlencoded",
	})
	if err != nil {
		t.Fatalf("POST 形式的回调应当被支持: %v", err)
	}
	if !res.Success {
		t.Error("POST 回调应当被正确解析")
	}
}

// TestCreatePayment_PageMode 验证页面跳转模式生成的表单。
func TestCreatePayment_PageMode(t *testing.T) {
	p := newTestProvider(t)
	res, err := p.CreatePayment(context.Background(), payment.PaymentRequest{
		OrderNo:   "20260826123456ABCD",
		Subject:   "测试商品 & 特殊=字符",
		Amount:    12345, // 123.45 元
		NotifyURL: "https://shop.example.com/api/v1/payments/notify/yipay_v1/1",
		ReturnURL: "https://shop.example.com/pay/result",
	})
	if err != nil {
		t.Fatalf("创建支付失败: %v", err)
	}
	if res.Action != payment.ActionForm {
		t.Errorf("页面跳转模式应返回 form，实际 %q", res.Action)
	}
	if res.FormHTML == "" {
		t.Error("表单 HTML 不应为空")
	}
	// 金额必须是元
	if !strings.Contains(res.FormHTML, "123.45") {
		t.Error("表单中应包含元为单位的金额 123.45")
	}
	// 商品名里的 & 和 = 会破坏签名串结构，必须被清理
	if strings.Contains(res.FormHTML, "&amp; 特殊=") {
		t.Error("商品名中的 & 与 = 应当被替换掉")
	}
}
