package utils

import (
	"crypto/rand"
	"encoding/hex"
	"math/big"
	"strings"
	"time"
)

// base32Alphabet 去掉了容易混淆的 0/O、1/I/L，便于用户手抄订单号。
const base32Alphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// GenerateOrderNo 生成全局唯一且不可预测的订单号。
//
// 格式：yyyyMMddHHmmss + 10 位随机字符，例如 20260826124530A7K2MQ9XB4
//
// 为什么不用自增 ID：连续 ID 会让攻击者通过遍历猜出他人订单号。
// 时间前缀便于运维排查与按日期分片，随机后缀保证不可枚举。
func GenerateOrderNo() string {
	var sb strings.Builder
	sb.Grow(24)
	sb.WriteString(time.Now().UTC().Format("20060102150405"))
	sb.WriteString(randomFromAlphabet(10, base32Alphabet))
	return sb.String()
}

// GenerateQueryToken 生成订单查询 Token（32 位十六进制 = 128 bit 熵）。
//
// 用户可凭它免邮箱直接查单，因此必须是密码学安全随机数，绝不可用时间戳派生。
func GenerateQueryToken() string {
	b := make([]byte, 16)
	mustRand(b)
	return hex.EncodeToString(b)
}

// RandomHex 生成 n 字节的随机十六进制字符串（长度为 2n）。
func RandomHex(n int) string {
	b := make([]byte, n)
	mustRand(b)
	return hex.EncodeToString(b)
}

// RandomCouponCode 生成易读的优惠券码。
func RandomCouponCode(n int) string {
	if n <= 0 {
		n = 10
	}
	return randomFromAlphabet(n, base32Alphabet)
}

// RandomFileName 生成上传文件名（不含扩展名）。
func RandomFileName() string {
	return time.Now().UTC().Format("20060102") + "_" + RandomHex(12)
}

func randomFromAlphabet(n int, alphabet string) string {
	max := big.NewInt(int64(len(alphabet)))
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			// crypto/rand 失败意味着系统熵源不可用，属于不可恢复故障。
			panic("crypto/rand unavailable: " + err.Error())
		}
		out[i] = alphabet[v.Int64()]
	}
	return string(out)
}

func mustRand(b []byte) {
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
}
