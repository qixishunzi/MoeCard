package middleware

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/service"
)

func contextWithRequestID(ctx context.Context, rid string) context.Context {
	return context.WithValue(ctx, logger.RequestIDKey, rid)
}

// AdminAuth 校验管理员 JWT。
//
// Token 通过 Authorization: Bearer 头传递，**不使用 Cookie**。
// 这不仅是习惯问题：浏览器不会自动带上 Authorization 头，
// 因此后台接口天然免疫 CSRF，不需要额外的 token 机制。
func AdminAuth(admins *service.AdminService) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("Authorization")
		token := ""
		if parts := strings.SplitN(strings.TrimSpace(raw), " ", 2); len(parts) == 2 &&
			strings.EqualFold(parts[0], "Bearer") {
			token = strings.TrimSpace(parts[1])
		}
		if token == "" {
			api.FailCode(c, api.CodeUnauthorized)
			return
		}

		admin, err := admins.ParseToken(c.Request.Context(), token)
		if err != nil {
			api.Fail(c, err)
			return
		}
		c.Set(ContextKeyAdmin, admin)
		c.Next()
	}
}

// CurrentAdmin 从上下文取出当前管理员。
func CurrentAdmin(c *gin.Context) *model.Admin {
	v, ok := c.Get(ContextKeyAdmin)
	if !ok {
		return nil
	}
	admin, _ := v.(*model.Admin)
	return admin
}
