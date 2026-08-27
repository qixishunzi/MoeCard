// Package turnstile 封装 Cloudflare Turnstile 的服务端校验。
//
// 只有一个供应商，所以这里不做 payment / notify 那样的注册表，
// 就是一个薄薄的 HTTP 客户端 —— 多一层抽象只会让人多绕一圈。
//
// 官方契约（https://developers.cloudflare.com/turnstile/get-started/server-side-validation/）：
//
//	POST https://challenges.cloudflare.com/turnstile/v0/siteverify
//	必填 secret、response；可选 remoteip、idempotency_key
//	令牌最长 2048 字符、有效期 300 秒、且**只能校验一次**
//	重放会得到 timeout-or-duplicate
package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// VerifyURL 是官方校验端点。
const VerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// MaxTokenLen 是官方规定的令牌长度上限。
//
// 超长的直接在本地拒掉，不必浪费一次外部请求 —— 也免得被人拿超大 body 当放大器。
const MaxTokenLen = 2048

// Result 是校验结果。
type Result struct {
	Success     bool     `json:"success"`
	ChallengeTS string   `json:"challenge_ts"`
	Hostname    string   `json:"hostname"`
	ErrorCodes  []string `json:"error-codes"`
	Action      string   `json:"action"`
	CData       string   `json:"cdata"`
	Metadata    Metadata `json:"metadata"`
}

// Metadata 里目前只关心一件事：这次校验是不是用测试密钥完成的。
type Metadata struct {
	// ResultWithTestingKey 为 true 说明用的是 Cloudflare 的测试密钥。
	//
	// 这一位很要紧：1x...AA 那把"永远通过"的测试密钥会放行**任何**令牌，
	// 包括随手编的字符串。要是有人把它留在了线上，验证码就完全是装饰品，
	// 而页面上一切正常，没人会发现。
	ResultWithTestingKey bool `json:"result_with_testing_key"`
}

// 常见错误码，用于把官方英文码翻成给管理员看的中文。
const (
	ErrMissingSecret   = "missing-input-secret"
	ErrInvalidSecret   = "invalid-input-secret"
	ErrMissingResponse = "missing-input-response"
	ErrInvalidResponse = "invalid-input-response"
	ErrBadRequest      = "bad-request"
	ErrTimeoutOrDup    = "timeout-or-duplicate"
	ErrInternal        = "internal-error"
)

// ErrNoToken 表示请求里根本没带令牌，无需外发。
var ErrNoToken = errors.New("未提供人机验证令牌")

// Verifier 执行校验。零值不可用，请用 New 构造。
type Verifier struct {
	client *http.Client
	url    string // 可替换，便于测试
}

// New 构造一个校验器。
//
// 超时给 10 秒：Cloudflare 的端点通常几十毫秒就回，
// 但它挂掉时不能让下单接口一直吊着 —— 那等于把自己的可用性绑在别人身上。
func New() *Verifier {
	return &Verifier{
		client: &http.Client{Timeout: 10 * time.Second},
		url:    VerifyURL,
	}
}

// WithEndpoint 换掉校验端点，仅供测试使用。
func (v *Verifier) WithEndpoint(u string) *Verifier {
	return &Verifier{client: v.client, url: u}
}

// Verify 校验一个令牌。
//
// remoteIP 传访客真实 IP（可选）。传不上来就留空 ——
// 官方允许省略，硬塞一个错的反而会导致误判。
func (v *Verifier) Verify(ctx context.Context, secret, token, remoteIP string) (*Result, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrNoToken
	}
	if len(token) > MaxTokenLen {
		return nil, fmt.Errorf("人机验证令牌超长（%d > %d）", len(token), MaxTokenLen)
	}
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("未配置 Turnstile 密钥")
	}

	form := url.Values{}
	form.Set("secret", secret)
	form.Set("response", token)
	// 只在拿得到公网可解析的 IP 时才带上。
	// 反代后面取到的可能是 127.0.0.1 或内网地址，带上去只会让 Cloudflare 困惑。
	if ip := net.ParseIP(strings.TrimSpace(remoteIP)); ip != nil && !ip.IsLoopback() && !ip.IsPrivate() {
		form.Set("remoteip", ip.String())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.url,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("构造校验请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接 Cloudflare 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Cloudflare 返回 HTTP %d", resp.StatusCode)
	}

	var out Result
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("解析校验结果失败: %w", err)
	}
	return &out, nil
}

// FriendlyError 把官方错误码翻成能直接给人看的话。
//
// 区分「配置错了」和「用户没过」很重要：前者要提醒管理员去后台改，
// 后者只该让访客重试一次，不能把两种情况混成一句"验证失败"。
func FriendlyError(codes []string) string {
	for _, c := range codes {
		switch c {
		case ErrMissingSecret, ErrInvalidSecret:
			return "人机验证配置有误（密钥不正确），请联系管理员"
		case ErrMissingResponse:
			return "请先完成人机验证"
		case ErrInvalidResponse:
			return "人机验证未通过，请重试"
		case ErrTimeoutOrDup:
			return "人机验证已过期或被重复使用，请重新验证"
		case ErrBadRequest:
			return "人机验证请求格式有误，请重试"
		case ErrInternal:
			return "Cloudflare 暂时不可用，请稍后重试"
		}
	}
	return "人机验证未通过，请重试"
}

// IsConfigError 判断失败是不是「站长把密钥配错了」。
//
// 用它把管理员该处理的问题和访客该重试的问题分开：
// 密钥配错时全站都过不了，得让日志和提示都指向配置，而不是让访客一直点重试。
func IsConfigError(codes []string) bool {
	for _, c := range codes {
		if c == ErrMissingSecret || c == ErrInvalidSecret {
			return true
		}
	}
	return false
}
