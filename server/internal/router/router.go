package router

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/config"
	"github.com/moecard/server/internal/handler/admin"
	"github.com/moecard/server/internal/handler/public"
	"github.com/moecard/server/internal/middleware"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/service"
	"github.com/moecard/server/internal/storage"
	"github.com/moecard/server/internal/turnstile"
)

// RouterDeps 是构建路由所需的依赖。
type RouterDeps struct {
	Config   *config.Config
	Services *service.Services
	Limiters *middleware.Limiters
	// SPA 是嵌入的前端处理器；为 nil 时只提供 API（前后端分离部署）
	SPA gin.HandlerFunc
	// Build 是构建信息，后台"关于"页展示。版本号是 -ldflags 注入的，
	// 只有 main 包拿得到，所以从那里传进来。
	Build api.BuildInfo
}

// maxBodySize 是普通 API 请求体上限。上传接口单独放宽。
const maxBodySize = 2 << 20 // 2MB

// uploadPath 是唯一需要放宽 body 上限的接口。
// maxUploadBody 要留出 multipart 边界与表单字段的开销，
// 因此比 STORAGE_MAX_SIZE（默认 5MB）宽一些，真正的大小校验在存储层。
const (
	uploadPath    = "/api/v1/admin/upload"
	maxUploadBody = 12 << 20 // 12MB
)

// NewRouter 构建完整的 HTTP 路由。
func NewRouter(deps RouterDeps) *gin.Engine {
	cfg := deps.Config
	svc := deps.Services
	lim := deps.Limiters

	// Gin 默认走 release 模式，除非开发者显式设了 GIN_MODE。
	//
	// debug 模式会在启动时把上百条路由连同内部函数名全部打出来。
	// 这些东西只有在排查"我的路由注册上了吗"时才有用，而这个程序是
	// 双击就能跑的单文件 —— 店家看到的应该是几行状态，不是一屏
	// github.com/moecard/server/internal/handler/... 的刷屏，
	// 更不该让那两条真正重要的警告（开发模式、未配 BASE_URL）被淹掉。
	//
	// 需要看路由表时：GIN_MODE=debug 启动即可。
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.RedirectTrailingSlash = true
	r.HandleMethodNotAllowed = false

	// gin 自带的代理解析一律关掉，客户端 IP 全部交给下面的中间件。
	//
	// 不能让两套逻辑并存：gin 的 ClientIP() 只认 X-Forwarded-For 和 X-Real-IP，
	// 而且它排在最前面。Cloudflare 回源时两个头都会带 —— CF-Connecting-IP 是
	// 它自己写死的真实客户端 IP，X-Forwarded-For 则是"把访客自己发来的值原样
	// 保留、再追加一个"。访客只要自带一个 X-Forwarded-For，最左边那个就是他
	// 编的。留着 gin 的解析等于让编的那个赢过 Cloudflare 写的那个。
	_ = r.SetTrustedProxies(nil)

	// 解析真实客户端 IP，必须排在所有会用到 IP 的中间件（日志、限流、
	// 人机验证）前面 —— 它改写 RemoteAddr，后面的 c.ClientIP() 才拿得到结果。
	r.Use(middleware.ClientIP(cfg.App.TrustProxy, svc.Setting))

	r.Use(middleware.RequestID())
	r.Use(middleware.Recovery())
	r.Use(middleware.Logger())
	r.Use(middleware.SecurityHeaders())
	r.Use(middleware.CORS(middleware.CORSConfig{
		AllowedOrigins: splitCSV(cfg.App.FrontendURL),
		// 开发模式下 vite 跑在另一个端口，必须放开跨域；生产环境一律同源
		AllowAnyOrigin: !cfg.IsProduction(),
	}))

	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			api.FailCode(c, api.CodeNotFound)
			return
		}
		if deps.SPA != nil {
			deps.SPA(c)
			return
		}
		c.JSON(http.StatusNotFound, api.Response{Code: api.CodeNotFound, Message: "页面不存在"})
	})

	// 健康检查：不做维护模式拦截，供负载均衡探活。
	//
	// 必须真的探一次数据库：只返回静态 ok 的话，数据库挂了依然是 200，
	// 负载均衡不会把故障实例摘掉，流量会一直打进来。
	r.GET("/health", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()

		if err := svc.Setting.PingDB(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "unhealthy", "version": api.Version, "database": "down",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status": "ok", "version": api.Version, "database": "up",
		})
	})

	// 上传的图片
	if local, ok := svc.Storage.(*storage.LocalStorage); ok {
		r.Static(local.URLPrefix(), local.Root())
	}

	pub := public.New(svc)
	adm := admin.New(svc, cfg.App.TrustProxy, deps.Build)

	// 人机验证。校验器无状态，全局一个即可；
	// 开关和密钥每次请求现读，改完配置立刻生效，不需要重启。
	ts := turnstile.New()

	v1 := r.Group("/api/v1")
	// 上传接口需要更大的上限，在下面单独设置；这里必须排除，否则永远轮不到它生效
	v1.Use(middleware.BodySizeLimit(maxBodySize, uploadPath))
	v1.Use(middleware.Maintenance(svc.Setting))

	registerPublicRoutes(v1, pub, adm, lim, svc.Setting, ts)
	registerAdminRoutes(v1, adm, svc, lim, ts)
	registerDocsRoutes(r)

	return r
}

// registerPublicRoutes 注册前台路由。
func registerPublicRoutes(v1 *gin.RouterGroup, h *public.Handler, adm *admin.Handler,
	lim *middleware.Limiters, cfg middleware.TurnstileConfig, ts *turnstile.Verifier) {
	// 初始化流程（只有系统尚未初始化时才有效）
	v1.GET("/setup/status", adm.SetupStatus)
	v1.POST("/setup", middleware.RateLimit(lim.Setup, middleware.ByIP("setup")), adm.Setup)

	v1.GET("/config", h.GetConfig)

	browse := v1.Group("")
	browse.Use(middleware.RateLimit(lim.Public, middleware.ByIP("public")))
	{
		browse.GET("/categories", h.ListCategories)
		browse.GET("/products", h.ListProducts)
		browse.GET("/products/:slug", h.GetProduct)
		browse.GET("/payments", h.ListPaymentChannels)
	}

	v1.POST("/coupons/verify",
		middleware.RateLimit(lim.Coupon, middleware.ByIP("coupon")),
		middleware.Turnstile(cfg, ts, model.TurnstileSceneCoupon), h.VerifyCoupon)

	// 下单：人机验证放在限流之后 —— 先用便宜的本地计数挡住洪水，
	// 再为剩下的请求付一次外部校验的开销
	v1.POST("/orders",
		middleware.RateLimit(lim.CreateOrder, middleware.ByIP("order_create")),
		middleware.Turnstile(cfg, ts, model.TurnstileSceneOrder), h.CreateOrder)

	orderQuery := v1.Group("/orders")
	orderQuery.Use(middleware.RateLimit(lim.QueryOrder, middleware.ByIP("order_query")))
	{
		// 带 token 的查询跳过验证码：那串 token 本身就是凭证，
		// 拿得到它的人不需要再证明自己是人
		orderQuery.GET("/query",
			middleware.TurnstileUnless(cfg, ts, model.TurnstileSceneQuery,
				func(c *gin.Context) bool {
					return strings.TrimSpace(c.Query("token")) != ""
				}), h.QueryOrder)
		orderQuery.GET("/:order_no/status", h.GetOrderStatus)
		orderQuery.POST("/:order_no/cancel", h.CancelOrder)
	}

	v1.POST("/orders/:order_no/pay",
		middleware.RateLimit(lim.Pay, middleware.ByIP("pay")), h.Pay)

	// 支付回调：**绝不能限流**。
	// 支付平台的重试通常很密集，限流会导致合法回调被拒，
	// 用户付了钱却收不到货。安全性完全由验签保证。
	v1.Any("/payments/notify/:provider/:channel_id", h.Notify)
	v1.GET("/payments/return/:provider/:channel_id", h.Return)
}

// registerAdminRoutes 注册后台路由。
func registerAdminRoutes(v1 *gin.RouterGroup, h *admin.Handler, svc *service.Services,
	lim *middleware.Limiters, ts *turnstile.Verifier) {
	a := v1.Group("/admin")

	// 登录：两层限流。
	//   1. 按 IP + 用户名：防止对单个账号爆破
	//   2. 按 IP：防止换着用户名喷，绕过第一层
	a.POST("/login",
		middleware.RateLimit(lim.LoginIP, middleware.ByIP("admin_login_ip")),
		middleware.RateLimit(lim.Login, middleware.ByIPAndField("admin_login", "username")),
		middleware.Turnstile(svc.Setting, ts, model.TurnstileSceneLogin),
		h.Login)

	auth := a.Group("")
	auth.Use(middleware.AdminAuth(svc.Admin))
	{
		auth.POST("/logout", h.Logout)
		auth.GET("/build", h.Build)
		auth.GET("/update/check", h.CheckUpdate)
		auth.GET("/profile", h.Profile)
		auth.PUT("/profile", h.UpdateProfile)
		auth.PUT("/profile/password", h.ChangePassword)

		// 两步验证
		auth.GET("/profile/totp", h.TOTPStatus)
		auth.POST("/profile/totp/setup", h.BeginTOTPSetup)
		auth.POST("/profile/totp/enable", h.EnableTOTP)
		auth.POST("/profile/totp/disable", h.DisableTOTP)

		auth.GET("/dashboard", h.Dashboard)
		auth.GET("/dashboard/trend", h.DashboardTrend)

		// 分类
		auth.GET("/categories", h.ListCategories)
		auth.POST("/categories", h.CreateCategory)
		auth.GET("/categories/:id", h.GetCategory)
		auth.PUT("/categories/:id", h.UpdateCategory)
		auth.DELETE("/categories/:id", h.DeleteCategory)
		auth.POST("/categories/:id/move", h.MoveCategoryProducts)

		// 商品
		auth.GET("/products", h.ListProducts)
		auth.POST("/products", h.CreateProduct)
		auth.GET("/products/:id", h.GetProduct)
		auth.PUT("/products/:id", h.UpdateProduct)
		auth.DELETE("/products/:id", h.DeleteProduct)
		auth.POST("/products/:id/status", h.SetProductStatus)
		auth.POST("/products/:id/stock", h.SetProductStock)

		// 卡密
		auth.GET("/products/:id/codes", h.ListCodes)
		auth.POST("/products/:id/codes", h.ImportCodes)
		auth.DELETE("/products/:id/codes", h.DeleteCodes)
		auth.GET("/products/:id/codes/stats", h.CodeStats)
		auth.GET("/products/:id/codes/export", h.ExportCodes)
		auth.DELETE("/codes/:id", h.DeleteCode)

		// 卡密总览：不绑定商品，供侧边栏的独立卡密页使用
		auth.GET("/codes", h.ListAllCodes)
		auth.POST("/codes", h.ImportAnyCodes)
		auth.DELETE("/codes", h.DeleteAnyCodes)
		auth.GET("/codes/stats", h.AllCodeStats)
		auth.GET("/codes/inventory", h.CodeInventory)
		auth.GET("/codes/export", h.ExportAllCodes)

		// 订单
		auth.GET("/orders", h.ListOrders)
		auth.GET("/orders/export", h.ExportOrders)
		auth.GET("/orders/:id", h.GetOrder)
		auth.POST("/orders/:id/deliver", h.DeliverOrder)
		auth.POST("/orders/:id/remark", h.RemarkOrder)
		auth.POST("/orders/:id/refund", h.RefundOrder)
		auth.POST("/orders/:id/resend-mail", h.ResendOrderMail)
		auth.DELETE("/orders/:id/attention", h.ClearOrderAttention)

		// 优惠券
		auth.GET("/coupons", h.ListCoupons)
		auth.POST("/coupons", h.CreateCoupon)
		auth.GET("/coupons/:id", h.GetCoupon)
		auth.PUT("/coupons/:id", h.UpdateCoupon)
		auth.DELETE("/coupons/:id", h.DeleteCoupon)
		auth.GET("/coupons/:id/usages", h.ListCouponUsages)

		// 支付渠道
		auth.GET("/payments/providers", h.ListPaymentProviders)
		auth.GET("/payments", h.ListPaymentChannels)
		auth.POST("/payments", h.CreatePaymentChannel)
		auth.GET("/payments/:id", h.GetPaymentChannel)
		auth.PUT("/payments/:id", h.UpdatePaymentChannel)
		auth.DELETE("/payments/:id", h.DeletePaymentChannel)
		auth.POST("/payments/:id/test", h.TestPaymentChannel)

		// 设置
		auth.GET("/settings", h.GetSettings)
		auth.GET("/settings/runtime", h.GetSettingsRuntime)
		auth.PUT("/settings", h.UpdateSettings)
		auth.POST("/settings/mail/test", h.TestMail)

		// 商家通知
		auth.GET("/notify/providers", h.ListNotifyProviders)
		auth.POST("/notify/test", h.TestNotify)

		// 人机验证：开启前先测一次，避免把自己也锁在门外
		auth.POST("/turnstile/test", h.TestTurnstile)

		// 管理员
		auth.GET("/admins", h.ListAdmins)
		auth.POST("/admins", h.CreateAdmin)
		auth.PUT("/admins/:id", h.UpdateAdmin)
		auth.DELETE("/admins/:id", h.DeleteAdmin)

		// 日志
		auth.GET("/logs/operations", h.ListOperationLogs)
		auth.GET("/logs/payments", h.ListPaymentLogs)
		auth.GET("/logs/emails", h.ListEmailLogs)
		auth.GET("/logs/notify", h.ListNotifyLogs)

		// 上传：body 上限单独放宽（已在 v1 层排除全局的 2MB 限制）
		auth.POST("/upload", middleware.BodySizeLimit(maxUploadBody), h.Upload)
	}
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
