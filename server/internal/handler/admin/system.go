package admin

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/mail"
	"github.com/moecard/server/internal/middleware"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/service"
	"github.com/moecard/server/internal/utils"
)

// ---- Dashboard ----

// Dashboard godoc
// @Router /api/v1/admin/dashboard [get]
func (h *Handler) Dashboard(c *gin.Context) {
	stats, err := h.svc.Dashboard.Stats(c.Request.Context())
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, stats)
}

// DashboardTrend godoc
// @Router /api/v1/admin/dashboard/trend [get]
func (h *Handler) DashboardTrend(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	points, err := h.svc.Dashboard.Trend(c.Request.Context(), days)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, points)
}

// ---- 系统设置 ----

// GetSettings godoc
// @Router /api/v1/admin/settings [get]
//
// 敏感项（SMTP 密码）返回脱敏值。
func (h *Handler) GetSettings(c *gin.Context) {
	api.OK(c, h.svc.Setting.AllMasked())
}

// runtimeInfo 是设置页要用到的运行期事实（不是配置项，改不了）。
type runtimeInfo struct {
	// TrustProxy 来自启动配置 TRUST_PROXY。关着的时候任何 IP 请求头都不看。
	TrustProxy bool `json:"trust_proxy"`
	// BuiltinIPHeaders 让前端直接展示内置顺序，不用在两处各维护一份。
	BuiltinIPHeaders []string `json:"builtin_ip_headers"`
	// DetectedIP / DetectedFrom 是"你这次请求被识别成了什么"，
	// 站长配完 CDN 刷新一下就能确认有没有生效。
	DetectedIP   string `json:"detected_ip"`
	DetectedFrom string `json:"detected_from"`
}

// GetSettingsRuntime godoc
// @Summary 设置页需要的运行期信息
// @Router /api/v1/admin/settings/runtime [get]
func (h *Handler) GetSettingsRuntime(c *gin.Context) {
	ip, from := middleware.DetectClientIP(c.Request, h.svc.Setting)
	if !h.trustProxy {
		// 不信任代理时头部根本不参与解析，这里也别显示一个用不上的结果
		ip, from = "", ""
	}
	if ip == "" {
		ip = c.ClientIP()
	}
	api.OK(c, runtimeInfo{
		TrustProxy:       h.trustProxy,
		BuiltinIPHeaders: middleware.BuiltinIPHeaders,
		DetectedIP:       ip,
		DetectedFrom:     from,
	})
}

// UpdateSettings godoc
// @Router /api/v1/admin/settings [put]
func (h *Handler) UpdateSettings(c *gin.Context) {
	var kv map[string]string
	if !api.BindJSON(c, &kv) {
		return
	}
	// 只接受已知配置项，防止污染出一堆无用的 key。
	// 通知渠道的 notify_cfg_* 是运行期动态登记的，所以走 IsKnownSettingKey 而不是静态表。
	filtered := make(map[string]string, len(kv))
	var unknown []string
	for k, v := range kv {
		if model.IsKnownSettingKey(k) {
			filtered[k] = v
		} else {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		api.FailCodef(c, api.CodeSettingInvalid, "存在未知配置项: %s", strings.Join(unknown, ", "))
		return
	}

	if err := h.svc.Setting.Update(c.Request.Context(), filtered); err != nil {
		api.Fail(c, err)
		return
	}

	// 记录改了哪些 key（不记值 —— 值里可能有 SMTP 密码）
	keys := make([]string, 0, len(filtered))
	for k := range filtered {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h.log(c, model.ActionUpdateSettings, "settings", "", "修改配置项: "+strings.Join(keys, ", "))

	api.OKMessage(c, "配置已保存", h.svc.Setting.AllMasked())
}

type testMailRequest struct {
	To         string `json:"to" binding:"required"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	FromEmail  string `json:"from_email"`
	FromName   string `json:"from_name"`
	Encryption string `json:"encryption"`
}

// TestMail godoc
// @Router /api/v1/admin/settings/mail/test [post]
//
// 允许传入尚未保存的 SMTP 配置，让管理员"先测通再保存"。
// password 留空或传脱敏值时，使用数据库中已保存的密码。
func (h *Handler) TestMail(c *gin.Context) {
	var req testMailRequest
	if !api.BindJSON(c, &req) {
		return
	}
	if err := utils.ValidateEmail(strings.TrimSpace(req.To)); err != nil {
		api.FailCodef(c, api.CodeValidation, "收件邮箱不合法: %s", err.Error())
		return
	}

	var cfg *mail.Config
	if strings.TrimSpace(req.Host) != "" {
		cfg = &mail.Config{
			Host: strings.TrimSpace(req.Host), Port: req.Port,
			Username: req.Username, Password: req.Password,
			FromEmail: strings.TrimSpace(req.FromEmail), FromName: req.FromName,
			Encryption: req.Encryption,
		}
	}

	if err := h.svc.Mail.SendTest(c.Request.Context(), strings.TrimSpace(req.To), cfg); err != nil {
		api.FailCodef(c, api.CodeMailSendFailed, "发送失败: %s", err.Error())
		return
	}
	h.log(c, model.ActionTestMail, "settings", "", "发送测试邮件到 "+utils.MaskEmail(req.To))
	api.OKMessage(c, "测试邮件已发送，请查收（也请检查垃圾邮件箱）", nil)
}

// ---- 管理员管理 ----

// ListAdmins godoc
// @Router /api/v1/admin/admins [get]
func (h *Handler) ListAdmins(c *gin.Context) {
	var p api.Pagination
	if !api.BindQuery(c, &p) {
		return
	}
	p.Normalize()

	list, total, err := h.svc.Admin.ListAdmins(c.Request.Context(), p.Offset(), p.Limit())
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Page(c, list, total, p)
}

// CreateAdmin godoc
// @Router /api/v1/admin/admins [post]
func (h *Handler) CreateAdmin(c *gin.Context) {
	var in service.AdminInput
	if !api.BindJSON(c, &in) {
		return
	}
	admin, err := h.svc.Admin.CreateAdmin(c.Request.Context(), &in)
	if err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionCreateAdmin, "admin", fmt.Sprint(admin.ID), "创建管理员: "+admin.Username)
	api.OK(c, admin)
}

// UpdateAdmin godoc
// @Router /api/v1/admin/admins/{id} [put]
func (h *Handler) UpdateAdmin(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	var in service.AdminInput
	if !api.BindJSON(c, &in) {
		return
	}
	admin, err := h.svc.Admin.UpdateAdmin(c.Request.Context(), id, &in)
	if err != nil {
		api.Fail(c, err)
		return
	}
	detail := "修改管理员: " + admin.Username
	if in.Password != "" {
		detail += "（重置了密码）"
	}
	h.log(c, model.ActionUpdateAdmin, "admin", fmt.Sprint(id), detail)
	api.OK(c, admin)
}

// DeleteAdmin godoc
// @Router /api/v1/admin/admins/{id} [delete]
func (h *Handler) DeleteAdmin(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	operator := middleware.CurrentAdmin(c)
	var operatorID uint64
	if operator != nil {
		operatorID = operator.ID
	}
	if err := h.svc.Admin.DeleteAdmin(c.Request.Context(), id, operatorID); err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionDeleteAdmin, "admin", fmt.Sprint(id), "删除管理员")
	api.OKMessage(c, "管理员已删除", nil)
}

// ---- 日志 ----

type opLogQuery struct {
	api.Pagination
	AdminID uint64 `form:"admin_id"`
	Action  string `form:"action"`
	Keyword string `form:"keyword"`
}

// ListOperationLogs godoc
// @Router /api/v1/admin/logs/operations [get]
func (h *Handler) ListOperationLogs(c *gin.Context) {
	var q opLogQuery
	if !api.BindQuery(c, &q) {
		return
	}
	q.Normalize()

	list, total, err := h.svc.Admin.ListOperationLogs(c.Request.Context(), repository.AdminLogQuery{
		AdminID: q.AdminID, Action: q.Action, Keyword: q.Keyword,
		Offset: q.Offset(), Limit: q.Limit(),
	})
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Page(c, list, total, q.Pagination)
}

type emailLogQuery struct {
	api.Pagination
	OrderNo string `form:"order_no"`
	Email   string `form:"email"`
	Status  string `form:"status"`
}

// ListEmailLogs godoc
// @Router /api/v1/admin/logs/emails [get]
func (h *Handler) ListEmailLogs(c *gin.Context) {
	var q emailLogQuery
	if !api.BindQuery(c, &q) {
		return
	}
	q.Normalize()

	list, total, err := h.svc.Mail.ListLogs(c.Request.Context(), repository.EmailLogQuery{
		OrderNo: q.OrderNo, Email: q.Email, Status: q.Status,
		Offset: q.Offset(), Limit: q.Limit(),
	})
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Page(c, list, total, q.Pagination)
}
