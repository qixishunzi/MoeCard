package payment

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultTimeout 是调用支付平台接口的超时时间。
//
// 必须设置：默认的 http.Client 没有超时，一旦支付网关挂起，
// 下单请求会一直占着连接和 goroutine，最终拖垮整个服务。
const DefaultTimeout = 20 * time.Second

var sharedTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	MaxIdleConns:          100,
	MaxIdleConnsPerHost:   10,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	ForceAttemptHTTP2:     true,
}

// NewHTTPClient 返回带超时的共享 client。
func NewHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &http.Client{Timeout: timeout, Transport: sharedTransport}
}

// maxResponseBytes 限制读取支付平台响应的大小，防止恶意/异常的超大响应打爆内存。
const maxResponseBytes = 2 << 20 // 2MB

// HTTPResult 是一次 HTTP 调用的结果。
type HTTPResult struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// DoRequest 发起 HTTP 请求并读取响应（带大小上限）。
func DoRequest(ctx context.Context, client *http.Client, req *http.Request) (*HTTPResult, error) {
	if client == nil {
		client = NewHTTPClient(DefaultTimeout)
	}
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("请求支付平台失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("读取支付平台响应失败: %w", err)
	}
	return &HTTPResult{StatusCode: resp.StatusCode, Header: resp.Header, Body: body}, nil
}

// PostForm 以 application/x-www-form-urlencoded 提交。
func PostForm(ctx context.Context, client *http.Client, endpoint string, params map[string]string) (*HTTPResult, error) {
	body := MapToValues(params).Encode()
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", UserAgent)
	return DoRequest(ctx, client, req)
}

// PostJSON 以 application/json 提交。
func PostJSON(ctx context.Context, client *http.Client, endpoint string, body []byte, headers map[string]string) (*HTTPResult, error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return DoRequest(ctx, client, req)
}

// GetURL 发起 GET 请求。
func GetURL(ctx context.Context, client *http.Client, endpoint string, params map[string]string, headers map[string]string) (*HTTPResult, error) {
	u := endpoint
	if len(params) > 0 {
		sep := "?"
		if strings.Contains(endpoint, "?") {
			sep = "&"
		}
		u = endpoint + sep + MapToValues(params).Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", UserAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return DoRequest(ctx, client, req)
}

// UserAgent 是本系统对外请求时的标识。
const UserAgent = "MoeCard/1.0 (+https://github.com/moecard)"

// BuildAutoSubmitForm 生成一个自动提交的 HTML 表单。
//
// 用于易支付 / 支付宝这类"表单跳转"支付方式：某些参数（如商品名）可能很长，
// 塞进 GET 的 query string 会超出 URL 长度限制或被网关截断，
// 用 POST 表单更稳妥。
//
// 所有值都经过 HTML 转义，防止参数里的引号闭合属性造成 XSS。
func BuildAutoSubmitForm(action string, params map[string]string, method string) string {
	if method == "" {
		method = http.MethodPost
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8">`)
	sb.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1">`)
	sb.WriteString(`<title>正在跳转到支付页面…</title></head>`)
	sb.WriteString(`<body style="font-family:system-ui,-apple-system,'Segoe UI',sans-serif;text-align:center;padding-top:80px;color:#555">`)
	sb.WriteString(`<p>正在跳转到支付页面，请稍候…</p>`)
	sb.WriteString(`<form id="moecard-pay" method="` + html.EscapeString(method) + `" action="` + html.EscapeString(action) + `">`)
	for _, k := range keys {
		sb.WriteString(`<input type="hidden" name="` + html.EscapeString(k) + `" value="` + html.EscapeString(params[k]) + `">`)
	}
	sb.WriteString(`</form>`)
	sb.WriteString(`<script>document.getElementById("moecard-pay").submit();</script>`)
	sb.WriteString(`<noscript><p>浏览器未启用 JavaScript，请手动点击下方按钮。</p>`)
	sb.WriteString(`<button onclick="document.getElementById('moecard-pay').submit()">继续支付</button></noscript>`)
	sb.WriteString(`</body></html>`)
	return sb.String()
}

// BuildQueryURL 把参数拼到 URL 上（值会做 URL 编码）。
func BuildQueryURL(base string, params map[string]string) string {
	if len(params) == 0 {
		return base
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + MapToValues(params).Encode()
}

// JoinURL 拼接 base 与 path，自动处理斜杠。
func JoinURL(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	path = strings.TrimSpace(path)
	if path == "" {
		return base
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

// ParseFormBody 解析 form-urlencoded 请求体。
func ParseFormBody(body []byte) (url.Values, error) {
	return url.ParseQuery(string(body))
}

// Atoi64 安全地把字符串转 int64，失败返回 0。
func Atoi64(s string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// TimestampSec 返回当前 Unix 秒（字符串）。
func TimestampSec() string {
	return strconv.FormatInt(time.Now().Unix(), 10)
}
