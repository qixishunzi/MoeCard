// Package admin 是管理后台的 HTTP handler。
//
// 所有写操作都会记录 admin_operation_logs，满足审计要求。
package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/middleware"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/selfupdate"
	"github.com/moecard/server/internal/service"
)

// Handler 持有后台所需的服务。
type Handler struct {
	svc *service.Services
	// trustProxy 来自启动配置。设置页要如实告诉站长：
	// 这个开关关着的时候，自定义 IP 请求头填了也不会生效。
	trustProxy bool
	// build 是构建信息，"关于"页展示
	build api.BuildInfo
}

// New 构造。
func New(svc *service.Services, trustProxy bool, build api.BuildInfo) *Handler {
	return &Handler{svc: svc, trustProxy: trustProxy, build: build}
}

// Build godoc
// @Summary 构建信息
// @Router /api/v1/admin/build [get]
func (h *Handler) Build(c *gin.Context) {
	api.OK(c, h.build)
}

// log 记录管理员操作日志。
func (h *Handler) log(c *gin.Context, action, targetType, targetID, detail string) {
	h.svc.Admin.WriteLog(c.Request.Context(), middleware.CurrentAdmin(c), c.ClientIP(),
		action, targetType, targetID, detail)
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	// TOTPCode 是两步验证码或一次性恢复码；未开启 2FA 的账号忽略此字段。
	TOTPCode string `json:"totp_code"`
}

// Login godoc
// @Summary 管理员登录
// @Router /api/v1/admin/login [post]
func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if !api.BindJSON(c, &req) {
		return
	}
	res, err := h.svc.Admin.LoginWithTOTP(c.Request.Context(),
		req.Username, req.Password, req.TOTPCode, c.ClientIP())
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, gin.H{
		"token":      res.Token,
		"expires_at": res.ExpiresAt,
		"admin":      res.Admin,
	})
}

// Logout godoc
// @Summary 管理员登出
// @Router /api/v1/admin/logout [post]
//
// JWT 是无状态的，服务端没有会话可销毁，所以登出必须让令牌本身失效：
// 只让前端丢掉 token 的话，那张令牌在服务端有效到过期为止 ——
// 在公用电脑上点了登出走人，之前被复制走的令牌照样能进后台。
// 实现是 token_version +1，因此该账号在其它设备上的登录会一并失效。
func (h *Handler) Logout(c *gin.Context) {
	admin := middleware.CurrentAdmin(c)
	h.log(c, model.ActionLogout, "admin", "", "管理员登出")
	// 先写日志再吊销：吊销之后本次请求的身份信息就作废了
	if admin != nil {
		if err := h.svc.Admin.Logout(c.Request.Context(), admin.ID); err != nil {
			api.Fail(c, err)
			return
		}
	}
	api.OKMessage(c, "已登出，该账号在其它设备上的登录也已失效", nil)
}

// Profile godoc
// @Summary 当前管理员信息
// @Router /api/v1/admin/profile [get]
func (h *Handler) Profile(c *gin.Context) {
	api.OK(c, middleware.CurrentAdmin(c))
}

// CheckUpdate godoc
// @Summary 检查有没有新版本
// @Router /api/v1/admin/update/check [get]
//
// 只查不装。装的动作要换掉磁盘上的可执行文件并重启进程，
// 那是命令行（moecard -update）的活 —— 让网页去重启自己所在的进程，
// 出问题时既难排查又容易把站点弄挂。
func (h *Handler) CheckUpdate(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()

	res, err := selfupdate.New(h.build.Version).Check(ctx)
	if err != nil {
		// 查不到不是错误状态 —— 内网部署本来就连不上 GitHub，
		// 没必要在后台弹一个红色报错吓人。
		api.OK(c, gin.H{
			"current":   h.build.Version,
			"available": false,
			"error":     err.Error(),
		})
		return
	}
	api.OK(c, res)
}

// UpdateProfile godoc
// @Summary 修改自己的昵称与头像
// @Router /api/v1/admin/profile [put]
func (h *Handler) UpdateProfile(c *gin.Context) {
	admin := middleware.CurrentAdmin(c)
	if admin == nil {
		api.FailCode(c, api.CodeUnauthorized)
		return
	}
	var in service.ProfileInput
	if !api.BindJSON(c, &in) {
		return
	}
	updated, err := h.svc.Admin.UpdateProfile(c.Request.Context(), admin.ID, &in)
	if err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionUpdateAdmin, "admin", fmt.Sprint(admin.ID), "修改了自己的资料")
	api.OK(c, updated)
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// ChangePassword godoc
// @Summary 修改自己的密码
// @Router /api/v1/admin/profile/password [put]
func (h *Handler) ChangePassword(c *gin.Context) {
	var req changePasswordRequest
	if !api.BindJSON(c, &req) {
		return
	}
	admin := middleware.CurrentAdmin(c)
	if admin == nil {
		api.FailCode(c, api.CodeUnauthorized)
		return
	}
	if err := h.svc.Admin.ChangePassword(c.Request.Context(),
		admin.ID, req.OldPassword, req.NewPassword, c.ClientIP()); err != nil {
		api.Fail(c, err)
		return
	}
	api.OKMessage(c, "密码修改成功，请重新登录", nil)
}

// SetupStatus godoc
// @Summary 是否需要初始化
// @Router /api/v1/setup/status [get]
func (h *Handler) SetupStatus(c *gin.Context) {
	need, err := h.svc.Admin.NeedsSetup(c.Request.Context())
	if err != nil {
		api.Fail(c, api.WrapError(api.CodeInternal, err))
		return
	}
	api.OK(c, gin.H{"need_setup": need})
}

// Setup godoc
// @Summary 首次初始化
// @Router /api/v1/setup [post]
//
// 只有在系统还没有任何管理员时才能调用。
// 密码强度由 utils.ValidatePasswordStrength 强制校验，
// 从源头杜绝 admin/admin 这类默认弱口令。
func (h *Handler) Setup(c *gin.Context) {
	var in service.SetupInput
	if !api.BindJSON(c, &in) {
		return
	}
	admin, err := h.svc.Admin.Setup(c.Request.Context(), &in, c.ClientIP())
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OKMessage(c, "初始化完成，请使用刚创建的账号登录", gin.H{"username": admin.Username})
}
