package stripe

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/moecard/server/internal/payment"
)

const testWebhookSecret = "whsec_testsecret1234567890"

func newTestProvider(t *testing.T) *Provider {
	t.Helper()
	p, err := New(map[string]string{
		"secret_key":     "sk_test_123",
		"webhook_secret": testWebhookSecret,
		"currency":       "usd",
	})
	if err != nil {
		t.Fatalf("构造 provider 失败: %v", err)
	}
	return p.(*Provider)
}

// signPayload 按 Stripe 规则构造签名头。
func signPayload(secret, body string, ts int64) string {
	sig := payment.HMACSHA256Hex(secret, strconv.FormatInt(ts, 10)+"."+body)
	return fmt.Sprintf("t=%d,v1=%s", ts, sig)
}

const completedEvent = `{
  "id": "evt_test_123",
  "type": "checkout.session.completed",
  "data": {
    "object": {
      "id": "cs_test_abc",
      "client_reference_id": "20260826123456ABCD",
      "amount_total": 1999,
      "currency": "usd",
      "payment_status": "paid",
      "payment_intent": "pi_test_xyz",
      "metadata": { "order_no": "20260826123456ABCD" }
    }
  }
}`

// TestWebhook_Valid 验证正确签名的 Webhook 被接受。
func TestWebhook_Valid(t *testing.T) {
	p := newTestProvider(t)
	now := time.Now().Unix()

	h := http.Header{}
	h.Set("Stripe-Signature", signPayload(testWebhookSecret, completedEvent, now))

	res, err := p.VerifyNotify(context.Background(), payment.NotifyRequest{
		Method: http.MethodPost, Header: h, Body: []byte(completedEvent),
		ContentType: "application/json",
	})
	if err != nil {
		t.Fatalf("合法 Webhook 不应报错: %v", err)
	}
	if !res.Success {
		t.Error("payment_status=paid 应当判定为支付成功")
	}
	if res.OrderNo != "20260826123456ABCD" {
		t.Errorf("订单号解析错误: %q", res.OrderNo)
	}
	if res.TradeNo != "pi_test_xyz" {
		t.Errorf("应使用 PaymentIntent 作为交易号，实际 %q", res.TradeNo)
	}
	// usd 是两位小数币种，1999 分 → 1999 分（1:1）
	if res.Amount != 1999 {
		t.Errorf("金额应为 1999 分，实际 %d", res.Amount)
	}
	if res.ResponseStatus != http.StatusOK {
		t.Errorf("应答状态码应为 200，实际 %d", res.ResponseStatus)
	}
}

// TestWebhook_InvalidSignature 验证签名校验的各种失败场景。
//
// Stripe Webhook 是完全公开的端点，签名是唯一防线。
func TestWebhook_InvalidSignature(t *testing.T) {
	p := newTestProvider(t)
	now := time.Now().Unix()

	cases := []struct {
		name string
		sig  string
		body string
	}{
		{"缺少签名头", "", completedEvent},
		{"签名格式错误", "garbage", completedEvent},
		{"只有时间戳没有签名", fmt.Sprintf("t=%d", now), completedEvent},
		{"用错误密钥签名", signPayload("whsec_wrong_secret", completedEvent, now), completedEvent},
		{"签名有效但内容被篡改", signPayload(testWebhookSecret, completedEvent, now),
			`{"id":"evt_x","type":"checkout.session.completed","data":{"object":{"amount_total":1}}}`},
		{"时间戳过旧（重放攻击）",
			signPayload(testWebhookSecret, completedEvent, now-3600), completedEvent},
		{"时间戳来自未来",
			signPayload(testWebhookSecret, completedEvent, now+3600), completedEvent},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			if tc.sig != "" {
				h.Set("Stripe-Signature", tc.sig)
			}
			res, err := p.VerifyNotify(context.Background(), payment.NotifyRequest{
				Method: http.MethodPost, Header: h, Body: []byte(tc.body),
				ContentType: "application/json",
			})
			if err == nil {
				t.Fatal("非法 Webhook 竟然通过了校验！")
			}
			if res != nil && res.Success {
				t.Fatal("非法 Webhook 不应返回 Success=true")
			}
		})
	}
}

// TestWebhook_MultipleSignatures 验证 secret 轮换期间的多签名场景。
func TestWebhook_MultipleSignatures(t *testing.T) {
	p := newTestProvider(t)
	now := time.Now().Unix()

	// Stripe 在轮换 secret 期间会同时发送多个 v1 签名，任一匹配即可
	wrong := payment.HMACSHA256Hex("whsec_old", strconv.FormatInt(now, 10)+"."+completedEvent)
	right := payment.HMACSHA256Hex(testWebhookSecret, strconv.FormatInt(now, 10)+"."+completedEvent)

	h := http.Header{}
	h.Set("Stripe-Signature", fmt.Sprintf("t=%d,v1=%s,v1=%s", now, wrong, right))

	res, err := p.VerifyNotify(context.Background(), payment.NotifyRequest{
		Method: http.MethodPost, Header: h, Body: []byte(completedEvent),
		ContentType: "application/json",
	})
	if err != nil {
		t.Fatalf("多签名中有一个正确就应当通过: %v", err)
	}
	if !res.Success {
		t.Error("应当判定为支付成功")
	}
}

// TestWebhook_UnpaidSession 验证未支付的 session 不触发发货。
func TestWebhook_UnpaidSession(t *testing.T) {
	p := newTestProvider(t)
	now := time.Now().Unix()
	body := `{"id":"evt_1","type":"checkout.session.completed","data":{"object":{
		"id":"cs_1","client_reference_id":"ORDER1","amount_total":1000,
		"currency":"usd","payment_status":"unpaid","payment_intent":"pi_1"}}}`

	h := http.Header{}
	h.Set("Stripe-Signature", signPayload(testWebhookSecret, body, now))

	res, err := p.VerifyNotify(context.Background(), payment.NotifyRequest{
		Method: http.MethodPost, Header: h, Body: []byte(body), ContentType: "application/json",
	})
	if err != nil {
		t.Fatalf("签名有效不应报错: %v", err)
	}
	if res.Success {
		t.Error("payment_status=unpaid 不应判定为支付成功")
	}
}

// TestWebhook_IrrelevantEvent 验证无关事件被安全忽略（但仍回 200）。
func TestWebhook_IrrelevantEvent(t *testing.T) {
	p := newTestProvider(t)
	now := time.Now().Unix()
	body := `{"id":"evt_2","type":"checkout.session.expired","data":{"object":{"id":"cs_2"}}}`

	h := http.Header{}
	h.Set("Stripe-Signature", signPayload(testWebhookSecret, body, now))

	res, err := p.VerifyNotify(context.Background(), payment.NotifyRequest{
		Method: http.MethodPost, Header: h, Body: []byte(body), ContentType: "application/json",
	})
	if err != nil {
		t.Fatalf("无关事件不应报错: %v", err)
	}
	if res.Success {
		t.Error("checkout.session.expired 不应判定为支付成功")
	}
	// 必须回 200，否则 Stripe 会一直重试这条无关事件
	if res.ResponseStatus != http.StatusOK {
		t.Errorf("无关事件也应回 200 让 Stripe 停止重试，实际 %d", res.ResponseStatus)
	}
}

// TestZeroDecimalCurrency 验证零小数币种（如 JPY）的金额换算。
func TestZeroDecimalCurrency(t *testing.T) {
	p, err := New(map[string]string{
		"secret_key": "sk_test", "webhook_secret": testWebhookSecret, "currency": "jpy",
	})
	if err != nil {
		t.Fatal(err)
	}
	prov := p.(*Provider)
	now := time.Now().Unix()

	// JPY 无小数：Stripe 收到的 amount_total=1000 表示 1000 日元
	// 内部统一存"分"，因此应还原为 100000
	body := `{"id":"e","type":"checkout.session.completed","data":{"object":{
		"id":"cs","client_reference_id":"O1","amount_total":1000,
		"currency":"jpy","payment_status":"paid","payment_intent":"pi"}}}`

	h := http.Header{}
	h.Set("Stripe-Signature", signPayload(testWebhookSecret, body, now))

	res, err := prov.VerifyNotify(context.Background(), payment.NotifyRequest{
		Method: http.MethodPost, Header: h, Body: []byte(body), ContentType: "application/json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Amount != 100000 {
		t.Errorf("JPY 1000 应还原为内部的 100000，实际 %d", res.Amount)
	}
}
