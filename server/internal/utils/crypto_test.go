package utils

import (
	"strings"
	"testing"
	"time"
)

// TestTOTPRFC6238Vectors 用 RFC 6238 附录 B 的官方测试向量校验实现。
//
// 官方向量是 8 位码，本项目用 6 位（与 Google Authenticator 一致），
// 因此取官方值的后 6 位。密钥为 ASCII "12345678901234567890"。
func TestTOTPRFC6238Vectors(t *testing.T) {
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" // base32("12345678901234567890")

	cases := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	}
	for _, c := range cases {
		got, err := TOTPCode(secret, time.Unix(c.unix, 0).UTC())
		if err != nil {
			t.Fatalf("T=%d 计算失败: %v", c.unix, err)
		}
		if got != c.want {
			t.Errorf("T=%d 期望 %s，实际 %s", c.unix, c.want, got)
		}
	}
}

func TestVerifyTOTP(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}

	now, err := TOTPCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyTOTP(secret, now) {
		t.Error("当前时间窗的验证码应当通过")
	}
	// 允许 ±1 个时间窗的时钟偏差
	prev, _ := TOTPCode(secret, time.Now().Add(-30*time.Second))
	if !VerifyTOTP(secret, prev) {
		t.Error("上一个时间窗的验证码应当通过（容忍时钟偏差）")
	}
	// 超出容忍范围
	old, _ := TOTPCode(secret, time.Now().Add(-5*time.Minute))
	if VerifyTOTP(secret, old) {
		t.Error("5 分钟前的验证码不应通过")
	}
	for _, bad := range []string{"", "000", "abcdef", "1234567", "999999x"} {
		if VerifyTOTP(secret, bad) {
			t.Errorf("非法验证码 %q 不应通过", bad)
		}
	}
}

func TestTOTPURI(t *testing.T) {
	uri := TOTPURI("MoeCard", "admin", "ABCDEFGH")
	for _, want := range []string{"otpauth://totp/", "secret=ABCDEFGH", "issuer=MoeCard", "digits=6", "period=30"} {
		if !strings.Contains(uri, want) {
			t.Errorf("otpauth URI 缺少 %q: %s", want, uri)
		}
	}
}

func TestRecoveryCodes(t *testing.T) {
	plain, hashed, err := GenerateRecoveryCodes(8)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 8 || len(hashed) != 8 {
		t.Fatalf("期望 8 个恢复码，实际 %d/%d", len(plain), len(hashed))
	}
	for _, p := range plain {
		for _, h := range hashed {
			if p == h {
				t.Fatal("恢复码明文不得等于其哈希")
			}
		}
	}
	if i := MatchRecoveryCode(hashed, plain[3]); i != 3 {
		t.Errorf("恢复码应匹配到下标 3，实际 %d", i)
	}
	// 小写也应能匹配（用户抄写时大小写不一定对）
	if i := MatchRecoveryCode(hashed, strings.ToLower(plain[5])); i != 5 {
		t.Errorf("小写恢复码应匹配到下标 5，实际 %d", i)
	}
	if i := MatchRecoveryCode(hashed, "XXXXX-XXXXX"); i != -1 {
		t.Errorf("无效恢复码应返回 -1，实际 %d", i)
	}
}

func TestDataEncryptionRoundTrip(t *testing.T) {
	t.Cleanup(func() { _ = InitDataEncryption("") })

	if err := InitDataEncryption("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if !DataEncryptionEnabled() {
		t.Fatal("配置密钥后应当处于启用状态")
	}

	const plain = "CARD-ABCD-1234-XYZ9"
	enc, err := Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if enc == plain {
		t.Fatal("密文不应等于明文")
	}
	if !IsEncrypted(enc) {
		t.Fatalf("密文应带 enc:v1: 前缀，实际 %q", enc)
	}
	if strings.Contains(enc, plain) {
		t.Fatal("密文中不应出现明文片段")
	}

	got, err := Decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if got != plain {
		t.Fatalf("解密结果不符：期望 %q，实际 %q", plain, got)
	}

	// 同一明文两次加密结果必须不同（随机 nonce）
	enc2, _ := Encrypt(plain)
	if enc == enc2 {
		t.Fatal("相同明文两次加密不应产生相同密文（nonce 复用）")
	}

	// 已是密文时不重复加密
	if again, _ := Encrypt(enc); again != enc {
		t.Fatal("对密文再次加密不应改变内容")
	}

	// 明文数据（加密开启前写入的）应原样返回，保证灰度期间可读
	if v, err := Decrypt("LEGACY-PLAIN-CODE"); err != nil || v != "LEGACY-PLAIN-CODE" {
		t.Fatalf("历史明文应原样返回，得到 %q / %v", v, err)
	}
}

func TestDecryptDetectsTampering(t *testing.T) {
	t.Cleanup(func() { _ = InitDataEncryption("") })
	_ = InitDataEncryption("key-one-key-one-key-one")

	enc, err := Encrypt("SENSITIVE-CARD-CODE")
	if err != nil {
		t.Fatal(err)
	}

	// 篡改密文
	tampered := enc[:len(enc)-4] + "AAAA"
	if _, err := Decrypt(tampered); err == nil {
		t.Error("被篡改的密文必须解密失败，而不是返回错误明文")
	}

	// 换一把密钥
	_ = InitDataEncryption("key-two-key-two-key-two")
	if _, err := Decrypt(enc); err == nil {
		t.Error("用错误的密钥必须解密失败")
	}

	// 完全不配密钥时，遇到密文必须报错而不是把密文当明文发出去
	_ = InitDataEncryption("")
	if _, err := Decrypt(enc); err == nil {
		t.Error("未配置密钥却遇到密文时必须报错")
	}
	if got := MustDecrypt(enc); !strings.Contains(got, "解密失败") {
		t.Errorf("MustDecrypt 应返回可辨识的占位提示，实际 %q", got)
	}
}

func TestEncryptDisabledIsPassthrough(t *testing.T) {
	t.Cleanup(func() { _ = InitDataEncryption("") })
	_ = InitDataEncryption("")

	const plain = "NO-KEY-CONFIGURED"
	enc, err := Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if enc != plain {
		t.Fatalf("未配置密钥时应原样返回明文，实际 %q", enc)
	}
}
