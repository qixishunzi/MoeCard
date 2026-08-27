package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/turnstile"
)

// TurnstileConfig 是中间件运行时需要的配置来源。
//
// 用接口而不是直接依赖 service.SettingService：middleware 被 service 依赖，
// 反过来引用会成环。这里只要三个问题的答案，接口就这么大。
type TurnstileConfig interface {
	// Enabled 返回总开关是否打开
	Enabled() bool
	// SceneOn 返回某个场景是否需要验证
	SceneOn(scene model.TurnstileScene) bool
	// Secret 返回 Turnstile 密钥
	Secret() string
}

// TokenField 是前端提交令牌用的 JSON 字段名。
//
// 同时也接受 Cloudflare 自己那套表单字段名和请求头，
// 方便用 curl / 第三方客户端对接时不用猜。
const TokenField = "turnstile_token"

// maxTokenBody 是为取令牌而读取的 body 上限。
// 令牌本身最长 2048，加上订单表单的其余字段，256KB 绰绰有余。
const maxTokenBody = 256 << 10

// Turnstile 返回某个场景的人机验证中间件。
//
// 三层短路，任何一层不满足就直接放行 —— 没配、没开、这个场景没勾，
// 都不该让访客多做一步动作，更不该因此把请求拦掉。
func Turnstile(cfg TurnstileConfig, verifier *turnstile.Verifier, scene model.TurnstileScene) gin.HandlerFunc {
	return TurnstileUnless(cfg, verifier, scene, nil)
}

// TurnstileUnless 同上，但允许调用方声明「这种请求不必验证」。
//
// 目前只有订单查询用到：带着免登录凭证（query_token）来的请求本身就
// 持有一个 128 位的秘密，没有任何可枚举的空间，再让人点一次验证码
// 只是在为难那个从自己收藏夹点回来的买家。验证码要挡的是
// 「订单号 + 邮箱」那条能被撞库的路径。
func TurnstileUnless(cfg TurnstileConfig, verifier *turnstile.Verifier,
	scene model.TurnstileScene, skip func(*gin.Context) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg == nil || verifier == nil || !cfg.Enabled() || !cfg.SceneOn(scene) {
			c.Next()
			return
		}
		if skip != nil && skip(c) {
			c.Next()
			return
		}

		secret := cfg.Secret()
		if strings.TrimSpace(secret) == "" {
			// 开了开关却没填密钥。这是配置事故，不是访客的错：
			// 放行会让开关形同虚设，拦掉又会把整个店铺锁死。
			// 选择拦掉并明确说明原因 —— 悄悄放行等于安全功能假装在工作。
			logger.L().Error("人机验证已开启但未配置密钥，请求被拒绝", "scene", scene)
			abortCaptcha(c, api.CodeCaptchaMisconfig,
				"人机验证已开启但未配置密钥，请在后台「商城设置 → 人机验证」中填写")
			return
		}

		token := tokenFromRequest(c)
		if token == "" {
			abortCaptcha(c, api.CodeCaptchaRequired, "请先完成人机验证")
			return
		}

		res, err := verifier.Verify(c.Request.Context(), secret, token, c.ClientIP())
		if err != nil {
			// 网络层失败（Cloudflare 挂了 / 超时）。
			// 这里必须拒绝而不是放行：验证码的意义就在于失败时挡住，
			// "出错就放过" 等于给攻击者一个只要打挂 Cloudflare 就能绕过的开关。
			logger.L().Warn("人机验证请求失败", "scene", scene, "error", err)
			abortCaptcha(c, api.CodeCaptchaFailed, "人机验证服务暂时不可用，请稍后重试")
			return
		}
		if !res.Success {
			if turnstile.IsConfigError(res.ErrorCodes) {
				logger.L().Error("Turnstile 密钥无效", "scene", scene, "codes", res.ErrorCodes)
				abortCaptcha(c, api.CodeCaptchaMisconfig, turnstile.FriendlyError(res.ErrorCodes))
				return
			}
			abortCaptcha(c, api.CodeCaptchaFailed, turnstile.FriendlyError(res.ErrorCodes))
			return
		}

		c.Next()
	}
}

func abortCaptcha(c *gin.Context, code api.ErrCode, msg string) {
	c.AbortWithStatusJSON(http.StatusBadRequest, api.Response{Code: code, Message: msg})
}

// tokenFromRequest 按顺序从请求头、查询串、JSON body 里找令牌。
//
// body 读完必须放回去 —— 它是一次性的流，不还原后面的 BindJSON 就什么都读不到。
func tokenFromRequest(c *gin.Context) string {
	// Cloudflare 表单默认的字段名，也作为请求头支持，方便非浏览器客户端
	if v := strings.TrimSpace(c.GetHeader("CF-Turnstile-Response")); v != "" {
		return v
	}
	if v := strings.TrimSpace(c.Query(TokenField)); v != "" {
		return v
	}
	if v := strings.TrimSpace(c.Query("cf-turnstile-response")); v != "" {
		return v
	}

	if c.Request.Body == nil ||
		!strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "application/json") {
		return ""
	}
	buf, err := io.ReadAll(io.LimitReader(c.Request.Body, maxTokenBody))
	c.Request.Body = io.NopCloser(bytes.NewReader(buf))
	if err != nil {
		return ""
	}

	var m map[string]any
	if json.Unmarshal(buf, &m) != nil {
		return ""
	}
	for _, k := range []string{TokenField, "cf-turnstile-response"} {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
