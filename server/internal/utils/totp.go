package utils

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP（RFC 6238）实现。
//
// 沿用项目里"支付协议不引 SDK"的同一判断：算法本身只有几十行标准库代码，
// 手写更可控、可审计、依赖更少。Google Authenticator / 1Password / Authy
// 默认参数就是 SHA-1 / 6 位 / 30 秒，这里保持一致，否则用户扫码后对不上。

const (
	totpDigits = 6
	totpPeriod = 30 * time.Second
	// totpSkew 允许前后各 1 个时间窗，容忍手机与服务器的时钟偏差。
	// 再放宽就等于把有效期拉长到 2 分半，得不偿失。
	totpSkew = 1
)

// base32NoPad 是 Authenticator 系 App 通用的密钥编码（无 = 填充）。
var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateTOTPSecret 生成 20 字节（160 bit）随机密钥，返回 base32 字符串。
func GenerateTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成 TOTP 密钥失败: %w", err)
	}
	return base32NoPad.EncodeToString(b), nil
}

// TOTPCode 计算指定时刻的验证码。
func TOTPCode(secret string, t time.Time) (string, error) {
	key, err := base32NoPad.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", fmt.Errorf("TOTP 密钥格式不正确: %w", err)
	}

	counter := uint64(t.Unix()) / uint64(totpPeriod.Seconds())
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	// 动态截断（RFC 4226 §5.4）
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	mod := uint32(1)
	for i := 0; i < totpDigits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", totpDigits, value%mod), nil
}

// VerifyTOTP 校验验证码，允许前后各一个时间窗。
//
// 比较用恒定时间：验证码只有 6 位，逐字节短路比较会泄露前缀是否正确，
// 配合高频重试足以把爆破空间从 10^6 降到 60 次。
func VerifyTOTP(secret, code string) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits || secret == "" {
		return false
	}
	now := time.Now()
	for i := -totpSkew; i <= totpSkew; i++ {
		want, err := TOTPCode(secret, now.Add(time.Duration(i)*totpPeriod))
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// TOTPURI 生成 otpauth:// 链接，前端据此渲染二维码。
func TOTPURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(int(totpPeriod.Seconds())))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// ---- 恢复码 ----

// GenerateRecoveryCodes 生成 n 个一次性恢复码。
//
// 手机丢了就再也进不去后台是最常见的 2FA 事故，恢复码是必须品。
// 返回明文供用户抄写，同时返回哈希用于落库 —— 明文绝不入库。
func GenerateRecoveryCodes(n int) (plain []string, hashed []string, err error) {
	if n <= 0 {
		n = 8
	}
	for i := 0; i < n; i++ {
		// 10 位 base32 字母数字，中间加短横便于抄写
		raw := randomFromAlphabet(10, base32Alphabet)
		code := raw[:5] + "-" + raw[5:]
		plain = append(plain, code)
		hashed = append(hashed, SHA256Hex(code))
	}
	return plain, hashed, nil
}

// MatchRecoveryCode 在哈希列表中查找匹配项，返回下标；未命中返回 -1。
func MatchRecoveryCode(hashes []string, code string) int {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return -1
	}
	want := SHA256Hex(code)
	for i, h := range hashes {
		if subtle.ConstantTimeCompare([]byte(h), []byte(want)) == 1 {
			return i
		}
	}
	return -1
}
