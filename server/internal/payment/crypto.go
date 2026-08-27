package payment

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// 本文件集中实现各支付平台通用的签名/加解密原语。
// 不引入任何支付 SDK —— 这些协议的密码学部分本身就只有标准库这几十行，
// 手写更可控、更易审计，也避免 SDK 版本升级把业务逻辑带崩。

// SortedQuery 把参数按 key 的 ASCII 升序拼成 a=b&c=d。
//
// 这是易支付、支付宝、以及绝大多数国内支付平台的签名串构造规则：
//   - 按参数名 ASCII 升序
//   - 剔除 exclude 中列出的字段（通常是 sign / sign_type）
//   - 剔除空值
//   - 值**不做** URL 编码
func SortedQuery(params map[string]string, exclude ...string) string {
	skip := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		skip[e] = true
	}
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if skip[k] || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(params[k])
	}
	return sb.String()
}

// ValuesToMap 把 url.Values 摊平成 map（取每个 key 的第一个值）。
func ValuesToMap(v url.Values) map[string]string {
	out := make(map[string]string, len(v))
	for k, vs := range v {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

// MapToValues 把 map 转成 url.Values。
func MapToValues(m map[string]string) url.Values {
	v := url.Values{}
	for k, s := range m {
		if s != "" {
			v.Set(k, s)
		}
	}
	return v
}

// MD5Hex 返回小写十六进制 MD5。
func MD5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// SHA256Hex 返回小写十六进制 SHA256。
func SHA256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// HMACSHA256Hex 返回 HMAC-SHA256 的小写十六进制。
func HMACSHA256Hex(secret, msg string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

// HMACSHA256Base64 返回 HMAC-SHA256 的 base64。
func HMACSHA256Base64(secret, msg string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(msg))
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

// SecureCompareHex 恒定时间比较两个签名字符串（大小写不敏感）。
//
// 必须用恒定时间比较：普通的 == 会在第一个不同字节处提前返回，
// 攻击者可以通过测量响应时间逐字节爆破出正确签名。
func SecureCompareHex(a, b string) bool {
	return hmac.Equal([]byte(strings.ToLower(strings.TrimSpace(a))), []byte(strings.ToLower(strings.TrimSpace(b))))
}

// SecureCompare 恒定时间比较（大小写敏感，用于 base64 签名）。
func SecureCompare(a, b string) bool {
	return hmac.Equal([]byte(strings.TrimSpace(a)), []byte(strings.TrimSpace(b)))
}

// ---- RSA ----

// ParsePrivateKey 解析 RSA 私钥。
//
// 兼容三种常见形态，因为不同平台/工具导出的格式不一样，
// 让用户去纠结 PKCS1 还是 PKCS8 是很糟糕的体验：
//   - PEM 格式的 PKCS#1（-----BEGIN RSA PRIVATE KEY-----）
//   - PEM 格式的 PKCS#8（-----BEGIN PRIVATE KEY-----）
//   - 无头无尾的裸 base64（支付宝开放平台常见）
func ParsePrivateKey(raw string) (*rsa.PrivateKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("私钥为空")
	}

	der, err := decodePEMOrBase64(raw, "PRIVATE KEY")
	if err != nil {
		return nil, err
	}

	if k, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return k, nil
	}
	anyKey, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("解析私钥失败（既不是 PKCS#1 也不是 PKCS#8）: %w", err)
	}
	k, ok := anyKey.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("私钥不是 RSA 类型")
	}
	return k, nil
}

// ParsePublicKey 解析 RSA 公钥。
//
// 兼容：
//   - PEM PKIX 公钥（-----BEGIN PUBLIC KEY-----）
//   - PEM 证书（-----BEGIN CERTIFICATE-----），自动取出其中的公钥
//   - 裸 base64
func ParsePublicKey(raw string) (*rsa.PublicKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("公钥为空")
	}

	// 证书形态：先解析证书再取公钥
	if strings.Contains(raw, "BEGIN CERTIFICATE") {
		block, _ := pem.Decode([]byte(raw))
		if block == nil {
			return nil, errors.New("证书 PEM 解析失败")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("解析证书失败: %w", err)
		}
		pk, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("证书公钥不是 RSA 类型")
		}
		return pk, nil
	}

	der, err := decodePEMOrBase64(raw, "PUBLIC KEY")
	if err != nil {
		return nil, err
	}

	if anyKey, err := x509.ParsePKIXPublicKey(der); err == nil {
		if pk, ok := anyKey.(*rsa.PublicKey); ok {
			return pk, nil
		}
		return nil, errors.New("公钥不是 RSA 类型")
	}
	if pk, err := x509.ParsePKCS1PublicKey(der); err == nil {
		return pk, nil
	}
	// 有些平台直接给证书的 DER
	if cert, err := x509.ParseCertificate(der); err == nil {
		if pk, ok := cert.PublicKey.(*rsa.PublicKey); ok {
			return pk, nil
		}
	}
	return nil, errors.New("解析公钥失败")
}

// ParseCertificate 解析 PEM 证书，用于微信支付平台证书。
func ParseCertificate(raw string) (*x509.Certificate, error) {
	raw = strings.TrimSpace(raw)
	block, _ := pem.Decode([]byte(raw))
	if block == nil {
		der, err := base64.StdEncoding.DecodeString(stripWhitespace(raw))
		if err != nil {
			return nil, errors.New("证书格式不正确")
		}
		return x509.ParseCertificate(der)
	}
	return x509.ParseCertificate(block.Bytes)
}

func decodePEMOrBase64(raw, pemType string) ([]byte, error) {
	if strings.Contains(raw, "-----BEGIN") {
		block, _ := pem.Decode([]byte(raw))
		if block == nil {
			return nil, fmt.Errorf("PEM 解析失败（期望 %s）", pemType)
		}
		return block.Bytes, nil
	}
	der, err := base64.StdEncoding.DecodeString(stripWhitespace(raw))
	if err != nil {
		return nil, fmt.Errorf("base64 解析失败: %w", err)
	}
	return der, nil
}

func stripWhitespace(s string) string {
	return strings.NewReplacer("\n", "", "\r", "", " ", "", "\t", "").Replace(s)
}

// SignRSA 用 RSA 私钥签名，返回 base64。
// alg 支持 "SHA256"（RSA2，推荐）与 "SHA1"（RSA，仅为兼容老商户）。
func SignRSA(key *rsa.PrivateKey, content, alg string) (string, error) {
	var (
		h      crypto.Hash
		digest []byte
	)
	switch strings.ToUpper(alg) {
	case "SHA1":
		sum := sha1.Sum([]byte(content))
		h, digest = crypto.SHA1, sum[:]
	default:
		sum := sha256.Sum256([]byte(content))
		h, digest = crypto.SHA256, sum[:]
	}
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, h, digest)
	if err != nil {
		return "", fmt.Errorf("RSA 签名失败: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// VerifyRSA 用 RSA 公钥验签（签名为 base64）。
func VerifyRSA(key *rsa.PublicKey, content, signatureB64, alg string) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signatureB64))
	if err != nil {
		return fmt.Errorf("%w: 签名不是合法 base64", ErrInvalidSignature)
	}
	var (
		h      crypto.Hash
		digest []byte
	)
	switch strings.ToUpper(alg) {
	case "SHA1":
		sum := sha1.Sum([]byte(content))
		h, digest = crypto.SHA1, sum[:]
	default:
		sum := sha256.Sum256([]byte(content))
		h, digest = crypto.SHA256, sum[:]
	}
	if err := rsa.VerifyPKCS1v15(key, h, digest, sig); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}
	return nil
}

// ---- AES-GCM（微信支付 APIv3 回调解密）----

// DecryptAESGCM 用 APIv3 密钥解密回调 resource。
//
// 微信 APIv3 的 resource 使用 AES-256-GCM：
//
//	key             = APIv3 密钥（32 字节）
//	nonce           = resource.nonce
//	associated data = resource.associated_data
//	ciphertext      = base64(resource.ciphertext)，末尾 16 字节是 auth tag
func DecryptAESGCM(apiV3Key, nonce, associatedData, ciphertextB64 string) ([]byte, error) {
	if len(apiV3Key) != 32 {
		return nil, fmt.Errorf("APIv3 密钥长度必须为 32 字节，当前 %d", len(apiV3Key))
	}
	ct, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, fmt.Errorf("密文 base64 解析失败: %w", err)
	}
	block, err := aes.NewCipher([]byte(apiV3Key))
	if err != nil {
		return nil, fmt.Errorf("创建 AES cipher 失败: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("创建 GCM 失败: %w", err)
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("nonce 长度不正确: %d", len(nonce))
	}
	plain, err := gcm.Open(nil, []byte(nonce), ct, []byte(associatedData))
	if err != nil {
		// GCM 校验失败说明密文被篡改或密钥不对 —— 必须当作攻击处理
		return nil, fmt.Errorf("%w: AES-GCM 解密失败（APIv3 密钥可能不正确）", ErrInvalidSignature)
	}
	return plain, nil
}

// DecryptRSAOAEP256 用私钥解密 RSA-OAEP(SHA-256) 密文。
//
// 对应 WebCrypto 的 { name: "RSA-OAEP", hash: "SHA-256" }，
// HashPay 的回调信封用它来加密那把一次性 AES 密钥。
//
// 注意与 EncryptRSAOAEP 的区别：那个用 SHA-1（微信的要求），这里必须是 SHA-256。
// 两者不通用 —— 用错哈希会解密失败且错误信息毫无指向性。
func DecryptRSAOAEP256(priv *rsa.PrivateKey, ciphertextB64 string) ([]byte, error) {
	ct, err := base64.StdEncoding.DecodeString(strings.TrimSpace(ciphertextB64))
	if err != nil {
		return nil, fmt.Errorf("密文不是合法 base64: %w", err)
	}
	plain, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, priv, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: RSA-OAEP(SHA-256) 解密失败（私钥可能不匹配）", ErrInvalidSignature)
	}
	return plain, nil
}

// DecryptAESGCMRaw 用给定的 key / iv 解密 AES-GCM 密文。
//
// 与 DecryptAESGCM 的区别：那个是微信 APIv3 专用（字符串密钥 + associated data），
// 这里是通用版本，密钥与 IV 都是原始字节。
//
// 约定认证标签**附在密文尾部** —— 这是 WebCrypto `crypto.subtle.encrypt`
// 的输出格式，也正是 Go `cipher.AEAD.Open` 期望的格式，两端天然兼容。
func DecryptAESGCMRaw(key, iv, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("创建 AES cipher 失败（密钥长度 %d 字节）: %w", len(key), err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, len(iv))
	if err != nil {
		return nil, fmt.Errorf("创建 GCM 失败（IV 长度 %d 字节）: %w", len(iv), err)
	}
	plain, err := gcm.Open(nil, iv, ciphertext, nil)
	if err != nil {
		// GCM 认证失败 = 密文或 IV 被篡改，必须当作攻击处理
		return nil, fmt.Errorf("%w: AES-GCM 认证失败，报文可能被篡改", ErrInvalidSignature)
	}
	return plain, nil
}

// EncryptRSAOAEP 用平台公钥加密敏感字段（微信部分接口要求，使用 SHA-1）。
func EncryptRSAOAEP(pub *rsa.PublicKey, plain string) (string, error) {
	ct, err := rsa.EncryptOAEP(sha1.New(), rand.Reader, pub, []byte(plain), nil)
	if err != nil {
		return "", fmt.Errorf("RSA-OAEP 加密失败: %w", err)
	}
	return base64.StdEncoding.EncodeToString(ct), nil
}

// RandomNonce 生成 32 位十六进制随机串（微信 Authorization 头需要）。
func RandomNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return strings.ToUpper(hex.EncodeToString(b))
}
