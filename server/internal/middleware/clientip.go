package middleware

import (
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// BuiltinIPHeaders 是内置的客户端 IP 请求头，按优先级从高到低。
//
// 顺序的依据是"越专用越可信"：CDN 自己注入的专用头部只有它能写，
// 而 X-Forwarded-For 是一条谁都能往里追加的链，放在最后兜底。
//
// 各家的头部名来自官方文档，不是猜的：
//   - CF-Connecting-IP     Cloudflare 回源时注入的原始客户端 IP
//   - EO-Connecting-IP     腾讯云 EdgeOne 默认回源头部
//   - EO-Client-IP         EdgeOne"自定义客户端 IP 头部"的官方示例名，用的人很多
//   - Ali-Real-Client-IP   阿里云 ESA（边缘安全加速）托管转换注入的默认头部
//   - Ali-CDN-Real-IP      阿里云 CDN 的专用回源头部
//   - True-Client-IP       Akamai，以及 Cloudflare 企业版的可选头部
//   - X-Real-IP            Nginx 反代最常见的写法
//   - X-Forwarded-For      通用标准，取最左边那个（最初的客户端）
var BuiltinIPHeaders = []string{
	"CF-Connecting-IP",
	"EO-Connecting-IP",
	"EO-Client-IP",
	"Ali-Real-Client-IP",
	"Ali-CDN-Real-IP",
	"True-Client-IP",
	"X-Real-IP",
	"X-Forwarded-For",
}

// ClientIPConfig 提供站长在后台配置的自定义头部。
//
// 做成接口而不是直接传字符串：这个配置在后台改完立即生效，
// 中间件每次请求都要读一次最新值。
type ClientIPConfig interface {
	// CustomIPHeaders 返回后台配置的头部名，优先于内置列表。
	CustomIPHeaders() []string
}

// ClientIP 解析真实客户端 IP，并把结果写回 RemoteAddr。
//
// 为什么改写 RemoteAddr 而不是存进 context：
// 全项目有十几处在调 c.ClientIP()（限流、下单留痕、Turnstile 校验、日志）。
// 换成自定义取值函数意味着这十几处都要改，而且以后任何人写下一个
// c.ClientIP() 都会悄悄绕过配置。改写 RemoteAddr 之后 gin 自己的
// ClientIP() 就返回解析结果，调用方一处都不用动。
//
// **只有在 trustProxy 打开时才看请求头。**默认不打开：
// 直接对公网暴露的部署里，任何人都能自己发一个 CF-Connecting-IP，
// 那样限流、风控、下单 IP 全部形同虚设。这一点没有折中余地 ——
// 站长得先明确声明"我在反代后面"，配置的头部才有意义。
func ClientIP(trustProxy bool, cfg ClientIPConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !trustProxy {
			c.Next()
			return
		}
		if ip, _ := DetectClientIP(c.Request, cfg); ip != "" {
			// 端口给 0：gin 只取冒号前面那段，端口在这里没有意义
			c.Request.RemoteAddr = net.JoinHostPort(ip, "0")
		}
		c.Next()
	}
}

// DetectClientIP 按"自定义头部 → 内置头部"的顺序找出第一个合法 IP，
// 并返回它来自哪个头部。
//
// 第二个返回值是给后台的自检用的：站长配完 CDN 之后最想知道的就是
// "服务器现在到底认哪个头部"，让他自己抓包比对不如直接告诉他。
func DetectClientIP(req *http.Request, cfg ClientIPConfig) (ip, header string) {
	var custom []string
	if cfg != nil {
		custom = cfg.CustomIPHeaders()
	}
	for _, name := range append(append([]string{}, custom...), BuiltinIPHeaders...) {
		if v := firstIPIn(req.Header, name); v != "" {
			return v, name
		}
	}
	return "", ""
}

// firstIPIn 取某个头部里第一个能解析成 IP 的值。
//
// 逗号分隔的链（X-Forwarded-For）取最左边：那是最初的客户端，
// 后面几个是沿途的代理。取错方向的话，所有请求看起来都来自同一台代理。
func firstIPIn(h http.Header, name string) string {
	raw := h.Get(name)
	if raw == "" {
		return ""
	}
	for _, part := range strings.Split(raw, ",") {
		if ip := normalizeIP(part); ip != "" {
			return ip
		}
	}
	return ""
}

// normalizeIP 校验并规范化一个 IP 字符串。
//
// 顺手剥掉端口和 IPv6 的方括号：有些代理会写成 1.2.3.4:5678 或 [::1]:80，
// 原样存进订单的话，后台看到的"下单 IP"就多一截没意义的端口号。
func normalizeIP(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
			return ip.String()
		}
	}
	return ""
}
