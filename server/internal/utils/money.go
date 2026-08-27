package utils

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// 金额全部以最小货币单位（分）的 int64 表示。
// 本文件是"分 ↔ 人民币字符串"之间唯一允许的转换点。

// FormatAmount 把分格式化为两位小数字符串，例如 1000 → "10.00"。
func FormatAmount(cents int64) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}
	s := fmt.Sprintf("%d.%02d", cents/100, cents%100)
	if neg {
		return "-" + s
	}
	return s
}

// FormatAmountWithSymbol 带货币符号格式化，例如 "¥10.00"。
func FormatAmountWithSymbol(cents int64, symbol string) string {
	if symbol == "" {
		symbol = "¥"
	}
	return symbol + FormatAmount(cents)
}

// ParseAmount 把 "10.00" / "10" / "10.5" 解析为分。
//
// 使用纯字符串解析而非 strconv.ParseFloat —— 浮点会在 "0.29" 这类值上产生
// 0.28999999 的偏差，进而在四舍五入时少收 1 分钱。
func ParseAmount(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("金额为空")
	}
	neg := false
	if strings.HasPrefix(s, "-") {
		neg, s = true, s[1:]
	} else if strings.HasPrefix(s, "+") {
		s = s[1:]
	}

	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	if intPart == "" {
		intPart = "0"
	}
	whole, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("金额格式错误: %s", s)
	}

	var frac int64
	if hasFrac {
		switch {
		case len(fracPart) == 0:
			frac = 0
		case len(fracPart) == 1:
			f, e := strconv.ParseInt(fracPart, 10, 64)
			if e != nil {
				return 0, fmt.Errorf("金额格式错误: %s", s)
			}
			frac = f * 10
		default:
			// 多于两位小数直接截断，不做四舍五入 —— 商城场景下宁可少收不可多收。
			f, e := strconv.ParseInt(fracPart[:2], 10, 64)
			if e != nil {
				return 0, fmt.Errorf("金额格式错误: %s", s)
			}
			frac = f
		}
	}

	total := whole*100 + frac
	if neg {
		total = -total
	}
	return total, nil
}

// AmountToYuanString 返回不带符号的元字符串，用于支付平台参数（如易支付 money）。
func AmountToYuanString(cents int64) string { return FormatAmount(cents) }

// AmountToStripeUnit 返回 Stripe 所需的最小单位金额。
//
// Stripe 对零小数货币（JPY、KRW 等）要求直接传整数金额，
// 对其余货币要求传"分"。我们内部本来就存分，因此只需处理零小数货币。
func AmountToStripeUnit(cents int64, currency string) int64 {
	if zeroDecimalCurrencies[strings.ToUpper(currency)] {
		return cents / 100
	}
	return cents
}

// StripeUnitToAmount 是 AmountToStripeUnit 的逆运算，用于回调金额比对。
func StripeUnitToAmount(v int64, currency string) int64 {
	if zeroDecimalCurrencies[strings.ToUpper(currency)] {
		return v * 100
	}
	return v
}

// zeroDecimalCurrencies 来自 Stripe 文档中的零小数货币列表。
var zeroDecimalCurrencies = map[string]bool{
	"BIF": true, "CLP": true, "DJF": true, "GNF": true, "JPY": true,
	"KMF": true, "KRW": true, "MGA": true, "PYG": true, "RWF": true,
	"UGX": true, "VND": true, "VUV": true, "XAF": true, "XOF": true, "XPF": true,
}
