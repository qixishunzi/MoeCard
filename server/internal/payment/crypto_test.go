package payment

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/url"
	"strings"
	"testing"
)

// TestSortedQuery 验证签名串构造规则。
//
// 这是易支付 / 支付宝签名的地基：一旦排序或空值处理错了，
// 所有支付都会验签失败，而且报错信息毫无指向性。
func TestSortedQuery(t *testing.T) {
	cases := []struct {
		name    string
		params  map[string]string
		exclude []string
		want    string
	}{
		{
			name:   "按 ASCII 升序排列",
			params: map[string]string{"money": "1.00", "name": "test", "pid": "1001"},
			want:   "money=1.00&name=test&pid=1001",
		},
		{
			name:    "剔除 sign 与 sign_type",
			params:  map[string]string{"a": "1", "sign": "xxx", "sign_type": "MD5", "b": "2"},
			exclude: []string{"sign", "sign_type"},
			want:    "a=1&b=2",
		},
		{
			name:   "剔除空值",
			params: map[string]string{"a": "1", "b": "", "c": "3"},
			want:   "a=1&c=3",
		},
		{
			name:   "大写字母排在小写之前（ASCII 序）",
			params: map[string]string{"b": "2", "A": "1", "a": "3", "B": "4"},
			want:   "A=1&B=4&a=3&b=2",
		},
		{
			name:   "值不做 URL 编码",
			params: map[string]string{"url": "https://a.com/b?c=d", "n": "中文名称"},
			want:   "n=中文名称&url=https://a.com/b?c=d",
		},
		{
			name:   "空 map",
			params: map[string]string{},
			want:   "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SortedQuery(tc.params, tc.exclude...); got != tc.want {
				t.Errorf("SortedQuery() = %q，期望 %q", got, tc.want)
			}
		})
	}
}

// TestMD5Sign 用彩虹易支付文档中的规则验证 MD5 签名。
func TestMD5Sign(t *testing.T) {
	params := map[string]string{
		"pid":          "1001",
		"type":         "alipay",
		"out_trade_no": "20260826123456",
		"notify_url":   "https://shop.example.com/notify",
		"return_url":   "https://shop.example.com/return",
		"name":         "VIP",
		"money":        "1.00",
		"sign":         "should-be-excluded",
		"sign_type":    "MD5",
	}
	key := "TESTKEY123"

	content := SortedQuery(params, "sign", "sign_type")
	want := "money=1.00&name=VIP&notify_url=https://shop.example.com/notify" +
		"&out_trade_no=20260826123456&pid=1001&return_url=https://shop.example.com/return&type=alipay"
	if content != want {
		t.Fatalf("签名串构造错误:\n实际: %s\n期望: %s", content, want)
	}

	sig := MD5Hex(content + key)
	if len(sig) != 32 {
		t.Errorf("MD5 结果应为 32 位十六进制，实际 %d 位", len(sig))
	}
	if sig != strings.ToLower(sig) {
		t.Error("MD5 结果必须是小写")
	}
	// 相同输入必须得到相同签名
	if MD5Hex(content+key) != sig {
		t.Error("MD5 签名不稳定")
	}
	// 输入变化必须导致签名变化
	if MD5Hex(content+"WRONGKEY") == sig {
		t.Error("不同密钥竟然产生了相同签名")
	}
}

// TestSecureCompare 验证恒定时间比较的正确性。
func TestSecureCompare(t *testing.T) {
	if !SecureCompareHex("ABCDEF", "abcdef") {
		t.Error("十六进制比较应当大小写不敏感")
	}
	if !SecureCompareHex(" abcdef ", "abcdef") {
		t.Error("应当忽略首尾空白")
	}
	if SecureCompareHex("abcdef", "abcdee") {
		t.Error("不同值不应相等")
	}
	if SecureCompareHex("abcdef", "") {
		t.Error("空串不应与非空串相等")
	}
	if !SecureCompare("Sig==", "Sig==") {
		t.Error("base64 签名应当精确匹配")
	}
	if SecureCompare("Sig==", "sig==") {
		t.Error("base64 签名比较必须大小写敏感")
	}
}

// TestRSASignVerify 验证 RSA2 签名与验签（支付宝 / 易支付 V2 / 微信共用）。
func TestRSASignVerify(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	privPEM := string(pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
	pubDER, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))

	content := "app_id=2021000000000000&method=alipay.trade.page.pay&total_amount=1.00"

	t.Run("PEM PKCS1 私钥签名并验签", func(t *testing.T) {
		priv, err := ParsePrivateKey(privPEM)
		if err != nil {
			t.Fatalf("解析私钥失败: %v", err)
		}
		sig, err := SignRSA(priv, content, "SHA256")
		if err != nil {
			t.Fatalf("签名失败: %v", err)
		}
		pub, err := ParsePublicKey(pubPEM)
		if err != nil {
			t.Fatalf("解析公钥失败: %v", err)
		}
		if err := VerifyRSA(pub, content, sig, "SHA256"); err != nil {
			t.Errorf("验签应当通过: %v", err)
		}
		// 内容被篡改必须验签失败
		if err := VerifyRSA(pub, content+"x", sig, "SHA256"); err == nil {
			t.Error("内容被篡改后验签竟然通过了")
		}
		// 签名被篡改必须失败
		if err := VerifyRSA(pub, content, "AAAA"+sig[4:], "SHA256"); err == nil {
			t.Error("签名被篡改后验签竟然通过了")
		}
	})

	t.Run("PKCS8 私钥", func(t *testing.T) {
		pkcs8, _ := x509.MarshalPKCS8PrivateKey(key)
		p8PEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}))
		if _, err := ParsePrivateKey(p8PEM); err != nil {
			t.Errorf("应当支持 PKCS#8 私钥: %v", err)
		}
	})

	t.Run("裸 base64 私钥（支付宝常见格式）", func(t *testing.T) {
		raw := base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(key))
		if _, err := ParsePrivateKey(raw); err != nil {
			t.Errorf("应当支持无 PEM 头的 base64 私钥: %v", err)
		}
	})

	t.Run("裸 base64 公钥", func(t *testing.T) {
		raw := base64.StdEncoding.EncodeToString(pubDER)
		if _, err := ParsePublicKey(raw); err != nil {
			t.Errorf("应当支持无 PEM 头的 base64 公钥: %v", err)
		}
	})

	t.Run("SHA1 签名（RSA 兼容模式）", func(t *testing.T) {
		priv, _ := ParsePrivateKey(privPEM)
		pub, _ := ParsePublicKey(pubPEM)
		sig, err := SignRSA(priv, content, "SHA1")
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyRSA(pub, content, sig, "SHA1"); err != nil {
			t.Errorf("SHA1 验签应当通过: %v", err)
		}
		// 算法不匹配必须失败
		if err := VerifyRSA(pub, content, sig, "SHA256"); err == nil {
			t.Error("用 SHA256 验证 SHA1 签名竟然通过了")
		}
	})

	t.Run("非法密钥被拒绝", func(t *testing.T) {
		if _, err := ParsePrivateKey(""); err == nil {
			t.Error("空私钥应当报错")
		}
		if _, err := ParsePrivateKey("not-a-key"); err == nil {
			t.Error("非法私钥应当报错")
		}
		if _, err := ParsePublicKey("garbage!!!"); err == nil {
			t.Error("非法公钥应当报错")
		}
	})
}

// TestAESGCM 验证微信 APIv3 回调解密。
func TestAESGCM(t *testing.T) {
	// 用 32 字节密钥（APIv3 要求）
	key := "01234567890123456789012345678901"

	t.Run("密钥长度必须为 32", func(t *testing.T) {
		if _, err := DecryptAESGCM("short", "nonce1234567", "", "AAAA"); err == nil {
			t.Error("非 32 字节密钥应当报错")
		}
	})

	t.Run("篡改的密文解密失败", func(t *testing.T) {
		// GCM 的认证标签保证密文被改动后一定解密失败
		_, err := DecryptAESGCM(key, "123456789012", "transaction",
			base64.StdEncoding.EncodeToString([]byte("this-is-not-valid-ciphertext-at-all")))
		if err == nil {
			t.Error("无效密文应当解密失败")
		}
		if !strings.Contains(err.Error(), ErrInvalidSignature.Error()) &&
			!strings.Contains(err.Error(), "解密失败") {
			t.Errorf("错误信息应指明解密失败，实际: %v", err)
		}
	})
}

// TestHMACSHA256 验证 Stripe Webhook 与 HashPay 使用的 HMAC。
func TestHMACSHA256(t *testing.T) {
	// RFC 4231 Test Case 1
	got := HMACSHA256Hex("\x0b\x0b\x0b\x0b\x0b\x0b\x0b\x0b\x0b\x0b\x0b\x0b\x0b\x0b\x0b\x0b\x0b\x0b\x0b\x0b", "Hi There")
	want := "b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7"
	if got != want {
		t.Errorf("HMAC-SHA256 结果错误:\n实际: %s\n期望: %s", got, want)
	}

	if HMACSHA256Hex("secret", "a") == HMACSHA256Hex("secret", "b") {
		t.Error("不同内容不应产生相同 HMAC")
	}
	if HMACSHA256Hex("secret1", "a") == HMACSHA256Hex("secret2", "a") {
		t.Error("不同密钥不应产生相同 HMAC")
	}
}

// TestValuesToMap 验证 url.Values 摊平。
func TestValuesToMap(t *testing.T) {
	v := url.Values{}
	v.Add("a", "1")
	v.Add("a", "2") // 重复 key 取第一个
	v.Set("b", "3")

	m := ValuesToMap(v)
	if m["a"] != "1" {
		t.Errorf("重复 key 应取第一个值，实际 %q", m["a"])
	}
	if m["b"] != "3" {
		t.Errorf("b = %q，期望 3", m["b"])
	}
}

// TestBuildAutoSubmitForm 验证自动提交表单的 XSS 防护。
func TestBuildAutoSubmitForm(t *testing.T) {
	html := BuildAutoSubmitForm("https://pay.example.com/submit.php", map[string]string{
		"name": `"><script>alert(1)</script>`,
		"pid":  "1001",
	}, "POST")

	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Error("参数值中的脚本标签必须被转义")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Error("应当看到被转义后的内容")
	}
	if !strings.Contains(html, `action="https://pay.example.com/submit.php"`) {
		t.Error("表单 action 不正确")
	}
	if !strings.Contains(html, `name="pid"`) || !strings.Contains(html, `value="1001"`) {
		t.Error("普通参数应当正常输出")
	}
}

// TestRegistry 验证 provider 注册表。
func TestRegistry(t *testing.T) {
	// 注意：本测试不导入 providers 包，所以注册表可能是空的。
	// 这里只验证注册表本身的行为不会 panic。
	if IsRegistered("definitely-not-a-provider") {
		t.Error("未注册的 provider 不应被认为存在")
	}
	if _, err := Build("definitely-not-a-provider", nil); err == nil {
		t.Error("构造未注册的 provider 应当报错")
	}
	if fields := SecretFields("definitely-not-a-provider"); len(fields) != 0 {
		t.Error("未注册 provider 的敏感字段列表应为空")
	}
}
