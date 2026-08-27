package utils

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost 12 在现代硬件上约 250ms，足以让离线爆破变得昂贵，
// 同时不至于让登录接口变慢到影响体验。
const bcryptCost = 12

// HashPassword 生成 bcrypt 哈希。禁止任何地方明文保存密码。
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(b), nil
}

// VerifyPassword 校验密码。bcrypt.CompareHashAndPassword 本身是恒定时间的。
func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// weakPasswords 是明确禁止的弱口令，防止生产环境出现 admin/admin。
var weakPasswords = map[string]bool{
	"admin": true, "admin123": true, "123456": true, "12345678": true,
	"password": true, "password123": true, "111111": true, "000000": true,
	"qwerty": true, "abc123": true, "root": true, "123456789": true,
	"admin888": true, "88888888": true, "moecard": true,
}

// ValidatePasswordStrength 校验密码强度。
//
// 规则：长度 >= 8；不在弱口令黑名单；至少包含两类字符（字母/数字/符号）。
func ValidatePasswordStrength(pwd string) error {
	if len(pwd) < 8 {
		return errors.New("密码长度至少 8 位")
	}
	if len(pwd) > 128 {
		return errors.New("密码长度不能超过 128 位")
	}
	if weakPasswords[strings.ToLower(pwd)] {
		return errors.New("该密码过于常见，请更换")
	}
	var hasLetter, hasDigit, hasSymbol bool
	for _, r := range pwd {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		default:
			hasSymbol = true
		}
	}
	kinds := 0
	for _, ok := range []bool{hasLetter, hasDigit, hasSymbol} {
		if ok {
			kinds++
		}
	}
	if kinds < 2 {
		return errors.New("密码需至少包含字母、数字、符号中的两类")
	}
	return nil
}

// SecureCompare 恒定时间字符串比较，用于邮箱/token 校验，避免时序侧信道。
func SecureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// MaskSecret 对敏感值脱敏：保留前 4 位，其余用 * 代替。
// 短值直接全部打码，避免泄露过多信息。
func MaskSecret(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= 4 {
		return strings.Repeat("*", len(r))
	}
	keep := 4
	if len(r) > 24 {
		keep = 6
	}
	return string(r[:keep]) + strings.Repeat("*", 8)
}

// MaskEmail 对邮箱脱敏：ab***@example.com
func MaskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return MaskSecret(email)
	}
	name, domain := email[:at], email[at:]
	r := []rune(name)
	if len(r) <= 2 {
		return strings.Repeat("*", len(r)) + domain
	}
	return string(r[:2]) + strings.Repeat("*", 3) + domain
}

// MaskCardCode 对卡密脱敏（后台列表用）。
func MaskCardCode(s string) string {
	r := []rune(s)
	if len(r) <= 6 {
		return strings.Repeat("*", len(r))
	}
	return string(r[:3]) + strings.Repeat("*", 6) + string(r[len(r)-3:])
}

// SecretPlaceholder 是前端收到的脱敏占位符。
//
// 后端保存配置时：若提交值等于该占位符（或为脱敏形态），说明用户没有修改，
// 必须保留数据库旧值 —— 否则会把真实密钥覆盖成一串星号，
// 这是"改了个无关配置结果支付全挂"的典型线上事故。
const SecretPlaceholder = "__MOECARD_UNCHANGED__"

// IsSecretUnchanged 判断提交上来的敏感值是否代表"未修改"。
func IsSecretUnchanged(submitted string) bool {
	if submitted == SecretPlaceholder {
		return true
	}
	s := strings.TrimSpace(submitted)
	if s == "" {
		return false
	}
	// 兼容前端直接回传脱敏后的显示值（形如 sk_l********）
	if strings.Count(s, "*") >= 6 && strings.HasSuffix(s, "********") {
		return true
	}
	// 全部由 * 组成
	if strings.Trim(s, "*") == "" {
		return true
	}
	return false
}

// sensitiveKeyPattern 匹配需要在日志中过滤的字段名。
var sensitiveKeyPattern = regexp.MustCompile(`(?i)(key|secret|password|passwd|token|private|sign|certificate|cert|credential|authorization|api_?key|app_?secret)`)

// IsSensitiveKey 判断字段名是否敏感。
func IsSensitiveKey(k string) bool { return sensitiveKeyPattern.MatchString(k) }

// SanitizeMap 过滤 map 中的敏感字段，返回可安全落库/打日志的副本。
//
// 注意：sign 字段虽然本身不是密钥，但也一并脱敏 —— 它是签名结果，
// 保留完整值对排错帮助有限，却可能在日志泄露时被用于重放分析。
func SanitizeMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		if IsSensitiveKey(k) {
			out[k] = MaskSecret(v)
		} else {
			out[k] = v
		}
	}
	return out
}

// SanitizeAnyMap 同 SanitizeMap，支持 any 值类型。
func SanitizeAnyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if IsSensitiveKey(k) {
			if s, ok := v.(string); ok {
				out[k] = MaskSecret(s)
			} else {
				out[k] = "***"
			}
			continue
		}
		if nested, ok := v.(map[string]any); ok {
			out[k] = SanitizeAnyMap(nested)
			continue
		}
		out[k] = v
	}
	return out
}

// SHA256Hex 返回 sha256 的十六进制串。
func SHA256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// CodeContentHash 计算卡密去重哈希（带 product_id 前缀，使不同商品可有相同卡密）。
func CodeContentHash(productID uint64, content string) string {
	return SHA256Hex(fmt.Sprintf("%d|%s", productID, content))
}
