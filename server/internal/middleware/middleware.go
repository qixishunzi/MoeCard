// Package middleware 提供 Gin 中间件。
package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/internal/utils"
)

// ContextKeyAdmin 是 gin.Context 中当前管理员的键。
const ContextKeyAdmin = "current_admin"

// RequestID 为每个请求分配唯一 ID，贯穿日志便于排查。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-ID")
		if rid == "" || len(rid) > 64 {
			rid = utils.RandomHex(8)
		}
		c.Set(string(logger.RequestIDKey), rid)
		c.Header("X-Request-ID", rid)
		// 放进 request context，让 service / repository 层的日志也能带上
		c.Request = c.Request.WithContext(
			contextWithRequestID(c.Request.Context(), rid))
		c.Next()
	}
}

// Logger 记录访问日志。
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		c.Next()

		status := c.Writer.Status()
		elapsed := time.Since(start)
		if raw != "" {
			path = path + "?" + raw
		}

		attrs := []any{
			"method", c.Request.Method,
			"path", utils.TrimAndLimit(path, 500),
			"status", status,
			"elapsed_ms", elapsed.Milliseconds(),
			"ip", c.ClientIP(),
			"request_id", c.GetString(string(logger.RequestIDKey)),
		}
		switch {
		case status >= 500:
			logger.L().Error("http", attrs...)
		case status >= 400:
			logger.L().Warn("http", attrs...)
		default:
			logger.L().Info("http", attrs...)
		}
	}
}

// Recovery 捕获 panic 并返回统一错误响应。
//
// 生产环境不返回堆栈 —— 堆栈会泄露文件路径与代码结构。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				logger.L().Error("panic recovered",
					"panic", r,
					"path", c.Request.URL.Path,
					"method", c.Request.Method,
					"request_id", c.GetString(string(logger.RequestIDKey)))
				if !c.Writer.Written() {
					c.AbortWithStatusJSON(http.StatusInternalServerError, api.Response{
						Code:    api.CodeInternal,
						Message: api.CodeInternal.Message(),
					})
				} else {
					c.Abort()
				}
			}
		}()
		c.Next()
	}
}

// CORSConfig 是跨域配置。
type CORSConfig struct {
	AllowedOrigins []string // 为空时只允许同源
	// AllowAnyOrigin 仅用于开发模式：前后端分离调试时端口不同，必须放开。
	// 生产环境绝不置为 true。
	AllowAnyOrigin bool
}

// CORS 处理跨域请求。
//
// 未配置 AllowedOrigins 时**只允许同源**（不下发 Access-Control-Allow-Origin），
// 而不是反射请求方的 Origin —— 单二进制部署下前端与 API 本来就同源，
// 反射任意 Origin 等于让任何网站都能读取本站接口的响应。
//
// 后台使用 Authorization 头而非 Cookie，因此不需要 credentials，
// 这也顺带让 CSRF 攻击无从下手。
func CORS(cfg CORSConfig) gin.HandlerFunc {
	allowed := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		o = strings.TrimRight(strings.TrimSpace(o), "/")
		if o != "" {
			allowed[o] = true
		}
	}

	return func(c *gin.Context) {
		origin := strings.TrimRight(c.GetHeader("Origin"), "/")
		if origin != "" {
			if allowed[origin] || (cfg.AllowAnyOrigin && len(allowed) == 0) {
				c.Header("Access-Control-Allow-Origin", origin)
			}
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type,Authorization,X-Request-ID")
			c.Header("Access-Control-Expose-Headers", "X-Request-ID")
			c.Header("Access-Control-Max-Age", "86400")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// SecurityHeaders 添加基础安全响应头。
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("X-XSS-Protection", "0") // 现代浏览器已移除该功能，显式关闭避免旧实现引入漏洞
		c.Next()
	}
}

// MaintenanceChecker 用于判断是否处于维护模式。
type MaintenanceChecker interface {
	MaintenanceMode() bool
	Get(key string) string
}

// Maintenance 在维护模式下拦截前台请求。
//
// 放行：后台接口、支付回调、配置接口。
// 支付回调必须放行 —— 维护期间拒绝回调会导致用户付了钱系统不知道。
func Maintenance(checker MaintenanceChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !checker.MaintenanceMode() {
			c.Next()
			return
		}
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/api/v1/admin") ||
			strings.HasPrefix(p, "/api/v1/payments/notify") ||
			strings.HasPrefix(p, "/api/v1/payments/return") ||
			strings.HasPrefix(p, "/api/v1/config") ||
			strings.HasPrefix(p, "/api/v1/setup") ||
			strings.HasPrefix(p, "/health") {
			c.Next()
			return
		}
		msg := checker.Get("maintenance_text")
		if strings.TrimSpace(msg) == "" {
			msg = api.CodeMaintenance.Message()
		}
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, api.Response{
			Code: api.CodeMaintenance, Message: msg,
		})
	}
}

// BodySizeLimit 限制请求体大小，防止超大 body 打爆内存。
//
// exempt 列出由各自路由单独设限的路径（如上传接口）。
// 必须显式排除：中间件按注册顺序执行，外层的小上限会先一步用 413 拒掉大请求，
// 内层放宽的上限根本没有机会生效 —— 结果就是"配置写着 5MB，实际只能传 2MB"。
func BodySizeLimit(maxBytes int64, exempt ...string) gin.HandlerFunc {
	skip := make(map[string]bool, len(exempt))
	for _, p := range exempt {
		skip[p] = true
	}
	return func(c *gin.Context) {
		if skip[c.Request.URL.Path] {
			c.Next()
			return
		}
		if c.Request.ContentLength > maxBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, api.Response{
				Code: api.CodeUploadTooLarge, Message: "请求内容过大",
			})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}
