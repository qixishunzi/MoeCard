package hashpay

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/moecard/server/internal/payment"
)

// ---------------------------------------------------------------------------
// 测试脚手架：用 Go 复现 HashPay 服务端（Cloudflare Workers / WebCrypto）的行为，
// 从而验证两端是否真的能互通。这比 mock 掉加解密有意义得多 ——
// 密钥格式、哈希算法、认证标签位置任何一个对不上，这些测试都会失败。
// ---------------------------------------------------------------------------

type testEnv struct {
	priv    *rsa.PrivateKey
	privPEM string
	server  *httptest.Server
	// 服务端收到的最近一次请求，用于断言签名
	lastMethod string
	lastPath   string
	lastTS     string
	lastSig    string
	lastBody   string
	lastMerch  string
	// 可被测试改写的响应
	createResp string
	orderResp  string
	orderCode  int
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	e := &testEnv{
		priv: priv,
		privPEM: string(pem.EncodeToMemory(&pem.Block{
			Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv),
		})),
		orderCode: http.StatusOK,
	}

	e.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = r.Body.Read(body)
		}
		e.lastMethod = r.Method
		e.lastPath = r.URL.Path + func() string {
			if r.URL.RawQuery != "" {
				return "?" + r.URL.RawQuery
			}
			return ""
		}()
		e.lastTS = r.Header.Get("X-Timestamp")
		e.lastSig = r.Header.Get("X-Signature")
		e.lastMerch = r.Header.Get("X-Merchant-Id")
		e.lastBody = string(body)

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == pathCreate:
			w.Write([]byte(e.createResp))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, pathOrder):
			w.WriteHeader(e.orderCode)
			w.Write([]byte(e.orderResp))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"key":"errors.not_found"}}`))
		}
	}))
	t.Cleanup(e.server.Close)
	return e
}

func (e *testEnv) provider(t *testing.T, overrides map[string]string) *Provider {
	t.Helper()
	cfg := map[string]string{
		"gateway":         e.server.URL,
		"merchant_id":     "M12345",
		"private_key":     e.privPEM,
		"currency":        "CNY",
		"verify_by_query": "0", // 默认关掉回查，让单测聚焦在解密本身
	}
	for k, v := range overrides {
		cfg[k] = v
	}
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("构造 provider 失败: %v", err)
	}
	return p.(*Provider)
}

// verifyServerSideSignature 复现服务端的验签逻辑：
//
//	[METHOD, pathname+search, timestamp, body].join("\n")
//	RSASSA-PKCS1-v1_5 + SHA-256，签名为标准 Base64
func (e *testEnv) verifyServerSideSignature(t *testing.T) {
	t.Helper()
	if e.lastSig == "" {
		t.Fatal("请求未携带 X-Signature")
	}
	signed := strings.Join([]string{e.lastMethod, e.lastPath, e.lastTS, e.lastBody}, "\n")
	sig, err := base64.StdEncoding.DecodeString(e.lastSig)
	if err != nil {
		t.Fatalf("签名不是标准 Base64: %v", err)
	}
	sum := sha256.Sum256([]byte(signed))
	if err := rsa.VerifyPKCS1v15(&e.priv.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("服务端验签失败（待签名串 %q）: %v", signed, err)
	}
}

// buildCallback 复现服务端 encryptCallbackEnvelope 的行为：
//
//	随机 AES-256 密钥 + 12 字节 IV，A256GCM 加密（标签附在密文尾部），
//	密钥再用商户公钥 RSA-OAEP(SHA-256) 加密，全部标准 Base64。
func buildCallback(t *testing.T, pub *rsa.PublicKey, payloadJSON string) []byte {
	t.Helper()

	aesKey := make([]byte, 32)
	if _, err := rand.Read(aesKey); err != nil {
		t.Fatal(err)
	}
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		t.Fatal(err)
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	// Seal 返回 密文||认证标签，与 WebCrypto 的输出一致
	ct := gcm.Seal(nil, iv, []byte(payloadJSON), nil)

	encKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pub, aesKey, nil)
	if err != nil {
		t.Fatal(err)
	}

	env, _ := json.Marshal(callbackEnvelope{
		Alg:  callbackAlgorithm,
		Key:  base64.StdEncoding.EncodeToString(encKey),
		IV:   base64.StdEncoding.EncodeToString(iv),
		Data: base64.StdEncoding.EncodeToString(ct),
	})
	return env
}

func notifyReq(body []byte, merchantID string) payment.NotifyRequest {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("X-HashPay-Encryption", callbackAlgorithm)
	h.Set("X-HashPay-Merchant", merchantID)
	h.Set("X-HashPay-Timestamp", strconv.FormatInt(1800000000, 10))
	return payment.NotifyRequest{
		Method: http.MethodPost, Header: h, Body: body,
		ContentType: "application/json", RemoteIP: "1.2.3.4",
	}
}

// ---------------------------------------------------------------------------
// 请求签名
// ---------------------------------------------------------------------------

// TestCreatePayment_SignatureAndBody 验证下单请求能通过服务端验签，且报文字段正确。
func TestCreatePayment_SignatureAndBody(t *testing.T) {
	e := newTestEnv(t)
	e.createResp = `{"checkoutUrl":"https://pay.example.com/checkout/ord_abc",
	  "order":{"id":"ord_abc","merchantNo":"20260826ABCD","amount":178.20,
	           "currency":"CNY","status":"pending","expiresAt":1800003600},
	  "reused":false}`

	p := e.provider(t, nil)
	res, err := p.CreatePayment(context.Background(), payment.PaymentRequest{
		OrderNo:   "20260826ABCD",
		Subject:   "Windows 11 <b>专业版</b>密钥",
		Amount:    17820, // 178.20
		Currency:  "CNY",
		NotifyURL: "https://shop.example.com/api/v1/payments/notify/hashpay/3",
		ReturnURL: "https://shop.example.com/pay/result",
	})
	if err != nil {
		t.Fatalf("创建支付失败: %v", err)
	}

	// 1. 服务端能验通签名 —— 证明待签名串拼接方式与 HashPay 一致
	e.verifyServerSideSignature(t)

	// 2. 商户号头
	if e.lastMerch != "M12345" {
		t.Errorf("X-Merchant-Id = %q，期望 M12345", e.lastMerch)
	}

	// 3. 签名用的路径必须与实际请求路径一致
	if e.lastPath != pathCreate {
		t.Errorf("请求路径 = %q，期望 %q", e.lastPath, pathCreate)
	}

	// 4. 报文字段
	var body map[string]any
	dec := json.NewDecoder(strings.NewReader(e.lastBody))
	dec.UseNumber()
	if err := dec.Decode(&body); err != nil {
		t.Fatalf("请求体不是合法 JSON: %v (%s)", err, e.lastBody)
	}
	if body["merchantNo"] != "20260826ABCD" {
		t.Errorf("merchantNo = %v", body["merchantNo"])
	}
	if body["callback"] != "https://shop.example.com/api/v1/payments/notify/hashpay/3" {
		t.Errorf("callback = %v", body["callback"])
	}
	if body["return_url"] != "https://shop.example.com/pay/result" {
		t.Errorf("return_url = %v", body["return_url"])
	}
	// 金额必须是十进制数值（不是分、不是浮点噪声）
	amt, ok := body["amount"].(json.Number)
	if !ok {
		t.Fatalf("amount 类型不对: %T (%v)", body["amount"], body["amount"])
	}
	if amt.String() != "178.20" {
		t.Errorf("amount = %q，期望 178.20（十进制元，不能有浮点误差）", amt.String())
	}
	// 商品名里的 HTML 必须被剥掉
	if d, _ := body["description"].(string); strings.Contains(d, "<b>") {
		t.Errorf("description 未剥离 HTML: %q", d)
	}

	// 5. 返回值
	if res.Action != payment.ActionRedirect {
		t.Errorf("Action = %q，期望 redirect", res.Action)
	}
	if res.URL != "https://pay.example.com/checkout/ord_abc" {
		t.Errorf("URL = %q", res.URL)
	}
	// 平台订单号必须落库，否则之后没法查询
	if res.TradeNo != "ord_abc" {
		t.Errorf("TradeNo = %q，期望 ord_abc（HashPay 的订单 id）", res.TradeNo)
	}
}

// TestAmountPrecision 验证金额换算不引入浮点误差。
//
// 0.29 这类值用 float64 处理会变成 0.28999999999999998，
// 一旦发给平台就会导致回调金额与订单金额对不上。
func TestAmountPrecision(t *testing.T) {
	cases := []struct {
		cents int64
		want  string
	}{
		{1, "0.01"},
		{29, "0.29"},
		{100, "1.00"},
		{17820, "178.20"},
		{999999, "9999.99"},
		{100000000, "1000000.00"},
	}
	for _, tc := range cases {
		got := string(amountToJSON(tc.cents))
		if got != tc.want {
			t.Errorf("amountToJSON(%d) = %q，期望 %q", tc.cents, got, tc.want)
		}
		// 往返必须精确
		back, err := parseAmountNumber(json.Number(got))
		if err != nil {
			t.Fatalf("parseAmountNumber(%q) 出错: %v", got, err)
		}
		if back != tc.cents {
			t.Errorf("往返失真: %d -> %q -> %d", tc.cents, got, back)
		}
	}

	// 平台可能返回简写形式（178.2 而不是 178.20）
	for _, s := range []string{"178.2", "178.20", "178"} {
		v, err := parseAmountNumber(json.Number(s))
		if err != nil {
			t.Fatalf("解析 %q 出错: %v", s, err)
		}
		want := int64(17820)
		if s == "178" {
			want = 17800
		}
		if v != want {
			t.Errorf("parseAmountNumber(%q) = %d，期望 %d", s, v, want)
		}
	}
}

// ---------------------------------------------------------------------------
// 回调解密
// ---------------------------------------------------------------------------

const paidPayload = `{"timestamp":1800000000,"payload":{
  "orderId":"ord_abc","merchantNo":"20260826ABCD","amount":178.20,
  "currency":"CNY","status":"paid","payment":{"asset":"USDT","network":"TRC20"}}}`

// TestVerifyNotify_Decrypt 验证能正确解开 HashPay 的加密信封。
func TestVerifyNotify_Decrypt(t *testing.T) {
	e := newTestEnv(t)
	p := e.provider(t, nil)

	body := buildCallback(t, &e.priv.PublicKey, paidPayload)
	res, err := p.VerifyNotify(context.Background(), notifyReq(body, "M12345"))
	if err != nil {
		t.Fatalf("合法回调不应报错: %v", err)
	}
	if !res.Success {
		t.Errorf("status=paid 应判定为支付成功，实际 Success=false（status=%q）", res.Status)
	}
	if res.OrderNo != "20260826ABCD" {
		t.Errorf("OrderNo = %q", res.OrderNo)
	}
	if res.TradeNo != "ord_abc" {
		t.Errorf("TradeNo = %q", res.TradeNo)
	}
	if res.Amount != 17820 {
		t.Errorf("Amount = %d，期望 17820 分", res.Amount)
	}
	if res.ResponseStatus != http.StatusOK {
		t.Errorf("应答状态码 = %d，HashPay 只认 2xx", res.ResponseStatus)
	}
}

// TestVerifyNotify_Rejects 验证各种非法回调都被拒绝。
func TestVerifyNotify_Rejects(t *testing.T) {
	e := newTestEnv(t)
	p := e.provider(t, nil)
	good := buildCallback(t, &e.priv.PublicKey, paidPayload)

	t.Run("商户号不匹配", func(t *testing.T) {
		if _, err := p.VerifyNotify(context.Background(), notifyReq(good, "M99999")); err == nil {
			t.Fatal("其他商户号的回调竟然通过了")
		}
	})

	t.Run("用别人的公钥加密", func(t *testing.T) {
		other, _ := rsa.GenerateKey(rand.Reader, 2048)
		body := buildCallback(t, &other.PublicKey, paidPayload)
		if _, err := p.VerifyNotify(context.Background(), notifyReq(body, "M12345")); err == nil {
			t.Fatal("用其他密钥加密的回调竟然解开了")
		}
	})

	t.Run("密文被篡改", func(t *testing.T) {
		var env callbackEnvelope
		_ = json.Unmarshal(good, &env)
		raw, _ := base64.StdEncoding.DecodeString(env.Data)
		raw[len(raw)/2] ^= 0xFF // 翻转中间一个字节
		env.Data = base64.StdEncoding.EncodeToString(raw)
		body, _ := json.Marshal(env)

		// GCM 的认证标签必须发现这次篡改
		if _, err := p.VerifyNotify(context.Background(), notifyReq(body, "M12345")); err == nil {
			t.Fatal("被篡改的密文竟然通过了 GCM 认证")
		}
	})

	t.Run("算法标识不对", func(t *testing.T) {
		var env callbackEnvelope
		_ = json.Unmarshal(good, &env)
		env.Alg = "AES-CBC"
		body, _ := json.Marshal(env)
		if _, err := p.VerifyNotify(context.Background(), notifyReq(body, "M12345")); err == nil {
			t.Fatal("未知算法标识竟然通过了")
		}
	})

	t.Run("信封字段缺失", func(t *testing.T) {
		body := []byte(`{"alg":"RSA-OAEP-256+A256GCM","key":"","iv":"","data":""}`)
		if _, err := p.VerifyNotify(context.Background(), notifyReq(body, "M12345")); err == nil {
			t.Fatal("空信封竟然通过了")
		}
	})

	t.Run("非 JSON 请求体", func(t *testing.T) {
		if _, err := p.VerifyNotify(context.Background(), notifyReq([]byte("garbage"), "M12345")); err == nil {
			t.Fatal("非法 JSON 竟然通过了")
		}
	})
}

// TestVerifyNotify_NotPaid 验证非 paid 状态不触发发货。
func TestVerifyNotify_NotPaid(t *testing.T) {
	e := newTestEnv(t)
	p := e.provider(t, nil)

	pending := strings.Replace(paidPayload, `"status":"paid"`, `"status":"pending"`, 1)
	body := buildCallback(t, &e.priv.PublicKey, pending)

	res, err := p.VerifyNotify(context.Background(), notifyReq(body, "M12345"))
	if err != nil {
		t.Fatalf("解密应当成功: %v", err)
	}
	if res.Success {
		t.Error("status=pending 不应判定为支付成功")
	}
}

// ---------------------------------------------------------------------------
// 回查确认（默认开启的安全机制）
// ---------------------------------------------------------------------------

// TestVerifyNotify_VerifyByQuery 验证「回调后回查」这道防线。
//
// HashPay 的回调只加密不签名，而商户公钥并不保密 ——
// 攻击者完全可以用它加密一份伪造的 "paid" 通知。
// 因此必须以只有持私钥者才能发出的签名查询结果为准。
func TestVerifyNotify_VerifyByQuery(t *testing.T) {
	t.Run("回调说已付但服务端说未付_不认账", func(t *testing.T) {
		e := newTestEnv(t)
		// 服务端的权威结果是 pending
		e.orderResp = `{"id":"ord_abc","merchantNo":"20260826ABCD","amount":178.20,
		                "currency":"CNY","status":"pending"}`
		p := e.provider(t, map[string]string{"verify_by_query": "1"})

		body := buildCallback(t, &e.priv.PublicKey, paidPayload)
		res, err := p.VerifyNotify(context.Background(), notifyReq(body, "M12345"))
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if res.Success {
			t.Fatal("回调声称 paid 但服务端是 pending —— 必须以服务端为准，不能发货")
		}
		e.verifyServerSideSignature(t) // 回查请求本身也必须是签名的
	})

	t.Run("服务端确认已付_正常发货", func(t *testing.T) {
		e := newTestEnv(t)
		e.orderResp = `{"id":"ord_abc","merchantNo":"20260826ABCD","amount":178.20,
		                "currency":"CNY","status":"paid"}`
		p := e.provider(t, map[string]string{"verify_by_query": "1"})

		body := buildCallback(t, &e.priv.PublicKey, paidPayload)
		res, err := p.VerifyNotify(context.Background(), notifyReq(body, "M12345"))
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if !res.Success {
			t.Fatal("服务端确认已付，应当发货")
		}
		if res.Amount != 17820 {
			t.Errorf("金额应以回查结果为准: %d", res.Amount)
		}
	})

	t.Run("回调金额被篡改_以回查为准", func(t *testing.T) {
		e := newTestEnv(t)
		e.orderResp = `{"id":"ord_abc","merchantNo":"20260826ABCD","amount":178.20,
		                "currency":"CNY","status":"paid"}`
		p := e.provider(t, map[string]string{"verify_by_query": "1"})

		// 伪造一份「金额 0.01 但状态 paid」的回调
		tampered := strings.Replace(paidPayload, `"amount":178.20`, `"amount":0.01`, 1)
		body := buildCallback(t, &e.priv.PublicKey, tampered)

		res, err := p.VerifyNotify(context.Background(), notifyReq(body, "M12345"))
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		// 金额必须来自回查，而不是回调里那个被改过的值。
		// 这样上层 HandlePaymentSuccess 的金额校验才有意义。
		if res.Amount != 17820 {
			t.Errorf("金额 = %d，必须以服务端回查结果 17820 为准", res.Amount)
		}
	})

	t.Run("回查失败_不认账并要求重试", func(t *testing.T) {
		e := newTestEnv(t)
		e.orderCode = http.StatusInternalServerError
		e.orderResp = `{"error":{"key":"errors.internal"}}`
		p := e.provider(t, map[string]string{"verify_by_query": "1"})

		body := buildCallback(t, &e.priv.PublicKey, paidPayload)
		res, err := p.VerifyNotify(context.Background(), notifyReq(body, "M12345"))
		if err == nil {
			t.Fatal("回查失败时不能确认支付")
		}
		if res != nil && res.Success {
			t.Fatal("回查失败时 Success 必须为 false")
		}
		// 返回 5xx 让 HashPay 重试，而不是 4xx 让它放弃
		if res != nil && res.ResponseStatus < 500 {
			t.Errorf("回查失败应返回 5xx 触发重试，实际 %d", res.ResponseStatus)
		}
	})
}

// ---------------------------------------------------------------------------
// 查询
// ---------------------------------------------------------------------------

// TestQueryPayment 验证主动查询走平台订单号。
func TestQueryPayment(t *testing.T) {
	e := newTestEnv(t)
	e.orderResp = `{"id":"ord_abc","merchantNo":"20260826ABCD","amount":178.20,
	                "currency":"CNY","status":"paid","paidAt":1800000500}`
	p := e.provider(t, nil)

	st, err := p.QueryPayment(context.Background(), payment.QueryRequest{
		OrderNo: "20260826ABCD", TradeNo: "ord_abc",
	})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if !st.Paid {
		t.Error("应判定为已支付")
	}
	if st.Amount != 17820 {
		t.Errorf("Amount = %d", st.Amount)
	}
	// 必须查的是平台订单号那个路径
	if e.lastPath != pathOrder+"ord_abc" {
		t.Errorf("查询路径 = %q，期望 %q", e.lastPath, pathOrder+"ord_abc")
	}
	e.verifyServerSideSignature(t)

	t.Run("没有平台订单号时返回未支付而不是报错", func(t *testing.T) {
		st, err := p.QueryPayment(context.Background(), payment.QueryRequest{OrderNo: "X"})
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if st.Paid {
			t.Error("没有平台订单号时不能判定为已支付")
		}
	})
}

// TestQueryPayment_WrappedResponse 验证兼容 {"order":{...}} 这种包一层的返回。
func TestQueryPayment_WrappedResponse(t *testing.T) {
	e := newTestEnv(t)
	e.orderResp = `{"order":{"id":"ord_x","merchantNo":"N1","amount":9.90,
	                "currency":"USDT","status":"paid"}}`
	p := e.provider(t, nil)

	st, err := p.QueryPayment(context.Background(), payment.QueryRequest{TradeNo: "ord_x"})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if !st.Paid || st.Amount != 990 || st.Currency != "USDT" {
		t.Errorf("解析结果不对: paid=%v amount=%d currency=%q", st.Paid, st.Amount, st.Currency)
	}
}

// TestRefund 验证退款返回不支持（HashPay 无退款接口，加密货币转账不可逆）。
func TestRefund(t *testing.T) {
	e := newTestEnv(t)
	p := e.provider(t, nil)
	if _, err := p.Refund(context.Background(), payment.RefundRequest{OrderNo: "X", Amount: 100}); err == nil {
		t.Fatal("HashPay 没有退款接口，应返回不支持")
	}
}

// TestNew_RejectsBadConfig 验证配置校验。
func TestNew_RejectsBadConfig(t *testing.T) {
	e := newTestEnv(t)

	if _, err := New(map[string]string{"merchant_id": "M1", "private_key": e.privPEM}); err == nil {
		t.Error("缺少网关地址应当报错")
	}
	if _, err := New(map[string]string{
		"gateway": e.server.URL, "merchant_id": "M1", "private_key": "not-a-key",
	}); err == nil {
		t.Error("非法私钥应当报错")
	}

	// verify_by_query 缺省时必须是开启的 —— 绝不能静默降级到不安全模式
	p, err := New(map[string]string{
		"gateway": e.server.URL, "merchant_id": "M1", "private_key": e.privPEM,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !p.(*Provider).verifyByQuery {
		t.Error("verify_by_query 未配置时必须默认开启")
	}
}
