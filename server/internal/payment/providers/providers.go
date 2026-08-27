// Package providers 通过空导入把所有支付渠道注册进 payment.Registry。
//
// 为什么需要这个包：payment 子包（如 payment/stripe）要导入 payment 拿到
// Provider 接口定义，所以 payment 不能反过来导入它们（循环依赖）。
// 各子包在 init() 中调用 payment.Register，由本包统一空导入触发。
//
// 新增支付渠道时，除了写好子包，只需在这里加一行 import。
package providers

import (
	_ "github.com/moecard/server/internal/payment/alipay"
	_ "github.com/moecard/server/internal/payment/hashpay"
	_ "github.com/moecard/server/internal/payment/stripe"
	_ "github.com/moecard/server/internal/payment/wechat"
	_ "github.com/moecard/server/internal/payment/yipayv1"
	_ "github.com/moecard/server/internal/payment/yipayv2"
)
