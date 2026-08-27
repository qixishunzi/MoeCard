package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// 静态加密（encryption at rest）。
//
// 目的：数据库文件泄露时，未售卡密与 TOTP 密钥不能被直接使用。
// 备份丢失、服务器被入侵、云盘误共享 —— 这些场景下明文卡密等于现金被直接搬走。
//
// 设计取舍：
//   - AES-256-GCM：带认证的加密，密文被篡改会解密失败而不是悄悄返回错误明文
//   - 每条记录独立随机 nonce，绝不复用
//   - 密文带 "enc:v1:" 前缀，可以和历史明文数据共存，支持灰度迁移
//   - 主密钥来自环境变量，绝不落库 —— 否则和不加密没有区别

const (
	// encPrefix 标记一条记录已加密。带版本号，将来换算法时可以平滑过渡。
	encPrefix = "enc:v1:"
	// nonceSize 是 GCM 标准 nonce 长度。
	nonceSize = 12
)

// ErrNoEncryptionKey 表示未配置主密钥。
var ErrNoEncryptionKey = errors.New("未配置数据加密密钥（DATA_ENCRYPTION_KEY）")

// ErrDecryptFailed 表示密文无法解密：密钥不对，或数据被篡改。
var ErrDecryptFailed = errors.New("解密失败：密钥不正确或数据已损坏")

var (
	encMu   sync.RWMutex
	encAEAD cipher.AEAD
)

// InitDataEncryption 初始化主密钥。key 为空表示不启用加密。
//
// 接受两种形式：
//   - 64 位十六进制（32 字节）——推荐，openssl rand -hex 32
//   - 任意长度口令 —— 用 SHA-256 派生成 32 字节
//
// 后者是为了让人不至于因为"生成不出合规密钥"而干脆不开加密；
// 但口令强度完全取决于用户，文档里必须写清楚推荐用前者。
func InitDataEncryption(key string) error {
	key = strings.TrimSpace(key)

	encMu.Lock()
	defer encMu.Unlock()

	if key == "" {
		encAEAD = nil
		return nil
	}

	var raw []byte
	if len(key) == 64 {
		if b, err := hex.DecodeString(key); err == nil {
			raw = b
		}
	}
	if raw == nil {
		sum := sha256.Sum256([]byte(key))
		raw = sum[:]
	}

	block, err := aes.NewCipher(raw)
	if err != nil {
		return fmt.Errorf("初始化加密密钥失败: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("初始化 GCM 失败: %w", err)
	}
	encAEAD = aead
	return nil
}

// DataEncryptionEnabled 返回是否已配置主密钥。
func DataEncryptionEnabled() bool {
	encMu.RLock()
	defer encMu.RUnlock()
	return encAEAD != nil
}

// IsEncrypted 判断一段存储值是否为密文。
func IsEncrypted(s string) bool { return strings.HasPrefix(s, encPrefix) }

// Encrypt 加密明文。未配置密钥时原样返回明文 —— 这样未开启加密的实例仍可正常工作。
func Encrypt(plain string) (string, error) {
	encMu.RLock()
	aead := encAEAD
	encMu.RUnlock()

	if aead == nil {
		return plain, nil
	}
	if plain == "" {
		return "", nil
	}
	// 已经是密文就不要再套一层
	if IsEncrypted(plain) {
		return plain, nil
	}

	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("生成 nonce 失败: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, []byte(plain), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt 解密。传入明文（无前缀）时原样返回，兼容加密开启前写入的历史数据。
func Decrypt(stored string) (string, error) {
	if !IsEncrypted(stored) {
		return stored, nil
	}

	encMu.RLock()
	aead := encAEAD
	encMu.RUnlock()

	if aead == nil {
		// 数据是密文但密钥没配 —— 通常是启动时漏了环境变量。
		// 这种情况必须报错而不是返回乱码，否则会把密文当卡密发给买家。
		return "", ErrNoEncryptionKey
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, encPrefix))
	if err != nil || len(raw) < nonceSize {
		return "", ErrDecryptFailed
	}
	plain, err := aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return "", ErrDecryptFailed
	}
	return string(plain), nil
}

// MustDecrypt 解密失败时返回占位提示而不是空串。
//
// 用于列表展示这类"失败也要继续渲染"的场景：
// 返回空串会让人以为卡密本来就是空的，进而做出错误处置。
func MustDecrypt(stored string) string {
	v, err := Decrypt(stored)
	if err != nil {
		return "[解密失败：密钥不匹配]"
	}
	return v
}
