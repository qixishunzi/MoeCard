package admin

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/middleware"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/notify"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/service"
	"github.com/moecard/server/internal/turnstile"
	"github.com/moecard/server/internal/utils"
)

// ---------------------------------------------------------------------------
// 商家通知
// ---------------------------------------------------------------------------

// ListNotifyProviders godoc
// @Summary 可用的通知渠道及其配置字段
// @Router /api/v1/admin/notify/providers [get]
func (h *Handler) ListNotifyProviders(c *gin.Context) {
	api.OK(c, notify.Descriptors())
}

type testNotifyRequest struct {
	Channel string            `json:"channel" binding:"required"`
	Config  map[string]string `json:"config"`
}

// TestNotify godoc
// @Summary 发送测试通知
// @Router /api/v1/admin/notify/test [post]
//
// 允许传入尚未保存的配置，让管理员"先测通再保存"。
func (h *Handler) TestNotify(c *gin.Context) {
	var req testNotifyRequest
	if !api.BindJSON(c, &req) {
		return
	}
	if err := h.svc.Notify.SendTest(c.Request.Context(), req.Channel, req.Config); err != nil {
		api.FailCodef(c, api.CodeInternal, "发送失败: %s", err.Error())
		return
	}
	h.log(c, model.ActionTestNotify, "notify", req.Channel, "发送测试通知: "+req.Channel)
	api.OKMessage(c, "测试通知已发送，请查收", nil)
}

type notifyLogQuery struct {
	api.Pagination
	Channel string `form:"channel"`
	Event   string `form:"event"`
	Status  string `form:"status"`
}

// ListNotifyLogs godoc
// @Summary 通知发送日志
// @Router /api/v1/admin/logs/notify [get]
func (h *Handler) ListNotifyLogs(c *gin.Context) {
	var q notifyLogQuery
	if !api.BindQuery(c, &q) {
		return
	}
	q.Normalize()

	list, total, err := h.svc.Notify.ListLogs(c.Request.Context(), repository.NotifyLogQuery{
		Channel: q.Channel, Event: q.Event, Status: q.Status,
		Offset: q.Offset(), Limit: q.Limit(),
	})
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Page(c, list, total, q.Pagination)
}

// ---------------------------------------------------------------------------
// 两步验证
// ---------------------------------------------------------------------------

// TOTPStatus godoc
// @Summary 查询两步验证状态
// @Router /api/v1/admin/profile/totp [get]
func (h *Handler) TOTPStatus(c *gin.Context) {
	admin := middleware.CurrentAdmin(c)
	st, err := h.svc.Admin.GetTOTPStatus(c.Request.Context(), admin.ID)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, st)
}

// BeginTOTPSetup godoc
// @Summary 获取两步验证二维码
// @Router /api/v1/admin/profile/totp/setup [post]
func (h *Handler) BeginTOTPSetup(c *gin.Context) {
	admin := middleware.CurrentAdmin(c)
	res, err := h.svc.Admin.BeginTOTPSetup(c.Request.Context(), admin.ID)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, res)
}

type totpCodeRequest struct {
	Code string `json:"code" binding:"required"`
}

// EnableTOTP godoc
// @Summary 开启两步验证
// @Router /api/v1/admin/profile/totp/enable [post]
func (h *Handler) EnableTOTP(c *gin.Context) {
	var req totpCodeRequest
	if !api.BindJSON(c, &req) {
		return
	}
	admin := middleware.CurrentAdmin(c)
	codes, err := h.svc.Admin.EnableTOTP(c.Request.Context(), admin.ID, req.Code, c.ClientIP())
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OKMessage(c, "两步验证已开启，请立即保存恢复码", gin.H{"recovery_codes": codes})
}

type disableTOTPRequest struct {
	Password string `json:"password" binding:"required"`
}

// DisableTOTP godoc
// @Summary 关闭两步验证
// @Router /api/v1/admin/profile/totp/disable [post]
func (h *Handler) DisableTOTP(c *gin.Context) {
	var req disableTOTPRequest
	if !api.BindJSON(c, &req) {
		return
	}
	admin := middleware.CurrentAdmin(c)
	if err := h.svc.Admin.DisableTOTP(c.Request.Context(), admin.ID, req.Password, c.ClientIP()); err != nil {
		api.Fail(c, err)
		return
	}
	api.OKMessage(c, "两步验证已关闭", nil)
}

// ---------------------------------------------------------------------------
// 数据导出
// ---------------------------------------------------------------------------

// csvWriter 准备一个带 UTF-8 BOM 的 CSV 响应。
//
// BOM 不能省：没有它 Excel 会用本地代码页打开，中文全是乱码 ——
// 这是"导出功能能不能用"级别的问题，不是细节。
func csvWriter(c *gin.Context, filename string) *csv.Writer {
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, filename, filename))
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
	return csv.NewWriter(c.Writer)
}

// ExportOrders godoc
// @Summary 导出订单为 CSV
// @Router /api/v1/admin/orders/export [get]
//
// 分批流式写出：一次性把几万条订单读进内存再序列化，
// 在小内存 VPS 上就是一次 OOM。
func (h *Handler) ExportOrders(c *gin.Context) {
	var q adminOrderQuery
	if !api.BindQuery(c, &q) {
		return
	}
	tz := h.svc.Setting.Timezone()
	symbol := h.svc.Setting.CurrencySymbol()

	w := csvWriter(c, "orders.csv")
	defer w.Flush()

	_ = w.Write([]string{
		"订单号", "状态", "商品", "数量", "原价", "优惠", "实付(" + symbol + ")",
		"买家邮箱", "支付方式", "支付流水号", "发货方式", "买家备注信息",
		"创建时间", "支付时间", "发货时间",
	})

	const batch = 500
	for offset := 0; ; offset += batch {
		list, _, err := h.svc.Order.List(c.Request.Context(), repository.OrderQuery{
			Status: q.Status, Keyword: q.Keyword, Email: q.Email,
			OrderNo: q.OrderNo, Provider: q.Provider, ProductID: q.ProductID,
			ProductKeyword: q.ProductKeyword, ChannelID: q.ChannelID,
			Offset: offset, Limit: batch,
		})
		if err != nil {
			// 头已经发出去了，无法再改 HTTP 状态码；
			// 在文件末尾写一行错误标记，让人知道这份导出不完整。
			_ = w.Write([]string{"# 导出中断: " + err.Error()})
			return
		}
		if len(list) == 0 {
			return
		}
		for _, o := range list {
			name, qty := "", ""
			if len(o.Items) > 0 {
				name = o.Items[0].ProductName
				qty = fmt.Sprint(o.Items[0].Quantity)
			}
			custom := ""
			if m := service.DecodeCustomData(o.CustomData); len(m) > 0 {
				parts := make([]string, 0, len(m))
				for k, v := range m {
					parts = append(parts, k+"="+v)
				}
				custom = strings.Join(parts, "; ")
			}
			_ = w.Write([]string{
				o.OrderNo,
				model.OrderStatusLabel(o.Status),
				name, qty,
				utils.FormatAmount(o.OriginalAmount),
				utils.FormatAmount(o.DiscountAmount),
				utils.FormatAmount(o.PayAmount),
				o.Email,
				o.PaymentProvider,
				o.PaymentTradeNo,
				o.DeliveryType,
				custom,
				utils.FormatInZone(o.CreatedAt, tz, "2006-01-02 15:04:05"),
				utils.FormatPtrInZone(o.PaidAt, tz, "2006-01-02 15:04:05"),
				utils.FormatPtrInZone(o.DeliveredAt, tz, "2006-01-02 15:04:05"),
			})
		}
		w.Flush()
		if len(list) < batch {
			return
		}
	}
}

// ---- 人机验证 ----

type turnstileTestRequest struct {
	// Token 是管理员在后台那个演示控件上验证后拿到的令牌
	Token string `json:"token" binding:"required"`
}

// TestTurnstile godoc
// @Summary 用当前配置校验一个令牌，确认密钥填对了
// @Router /api/v1/admin/turnstile/test [post]
//
// 存在的意义：Site Key 和 Secret Key 必须是同一个 widget 的一对。
// 配错了的话，网站会在开启的那一刻对所有人失效 —— 包括管理员自己，
// 连登录页都进不去。所以要能在开启之前先试一次。
func (h *Handler) TestTurnstile(c *gin.Context) {
	var req turnstileTestRequest
	if !api.BindJSON(c, &req) {
		return
	}
	secret := h.svc.Setting.Secret()
	if secret == "" {
		api.FailCodef(c, api.CodeSettingInvalid, "请先填写并保存密钥（Secret Key）")
		return
	}

	res, err := turnstile.New().Verify(c.Request.Context(), secret, req.Token, c.ClientIP())
	if err != nil {
		api.FailCodef(c, api.CodeCaptchaFailed, "校验失败：%s", err.Error())
		return
	}
	if !res.Success {
		api.FailCodef(c, api.CodeCaptchaFailed, "%s（错误码：%s）",
			turnstile.FriendlyError(res.ErrorCodes), strings.Join(res.ErrorCodes, ", "))
		return
	}
	h.log(c, model.ActionUpdateSettings, "setting", "turnstile", "测试人机验证配置")

	msg := "验证通过，配置可用"
	if res.Metadata.ResultWithTestingKey {
		// 测试密钥会放行任何令牌，随手编一个字符串也能过。
		// 留在线上等于验证码根本没开，而页面上看不出任何异常 —— 必须说破。
		msg = "验证通过，但你填的是 Cloudflare 的测试密钥：它会放行任何请求，" +
			"正式上线前请换成自己站点的密钥"
	}
	api.OKMessage(c, msg, gin.H{
		"hostname":     res.Hostname,
		"challenge_ts": res.ChallengeTS,
		"testing_key":  res.Metadata.ResultWithTestingKey,
	})
}

// ExportAllCodes godoc
// @Summary 按当前筛选条件导出卡密为 CSV（跨商品）
// @Router /api/v1/admin/codes/export [get]
//
// 比单商品导出多一列商品名 —— 不然导出的文件混着几十个商品的卡密，
// 打开之后根本分不清哪条属于谁。
func (h *Handler) ExportAllCodes(c *gin.Context) {
	tz := h.svc.Setting.Timezone()
	productID := uint64(0)
	if v, err := strconv.ParseUint(c.Query("product_id"), 10, 64); err == nil {
		productID = v
	}

	w := csvWriter(c, "codes.csv")
	defer w.Flush()
	_ = w.Write([]string{"商品", "卡密内容", "状态", "关联订单", "导入时间", "售出时间"})

	const batch = 500
	for offset := 0; ; offset += batch {
		// reveal=true：导出的目的就是拿到完整卡密，脱敏后的文件毫无意义
		list, _, err := h.svc.Code.List(c.Request.Context(), repository.CodeQuery{
			ProductID: productID,
			Status:    c.Query("status"),
			Keyword:   c.Query("keyword"),
			OrderNo:   c.Query("order_no"),
			Offset:    offset,
			Limit:     batch,
		}, true)
		if err != nil {
			_ = w.Write([]string{"# 导出中断: " + err.Error()})
			return
		}
		if len(list) == 0 {
			return
		}
		for _, x := range list {
			name := x.ProductName
			if name == "" {
				name = fmt.Sprintf("#%d", x.ProductID)
			}
			_ = w.Write([]string{
				name,
				x.Content, // List 返回的已是解密后的明文
				x.Status,
				x.OrderNo,
				utils.FormatInZone(x.CreatedAt, tz, "2006-01-02 15:04:05"),
				utils.FormatPtrInZone(x.SoldAt, tz, "2006-01-02 15:04:05"),
			})
		}
		w.Flush()
		if len(list) < batch {
			return
		}
	}
}

// ExportCodes godoc
// @Summary 导出某商品的卡密为 CSV
// @Router /api/v1/admin/products/{id}/codes/export [get]
func (h *Handler) ExportCodes(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	tz := h.svc.Setting.Timezone()

	w := csvWriter(c, fmt.Sprintf("codes-%d.csv", id))
	defer w.Flush()
	_ = w.Write([]string{"卡密内容", "状态", "关联订单", "导入时间", "售出时间"})

	const batch = 500
	for offset := 0; ; offset += batch {
		// reveal=true：导出的目的就是拿到完整卡密，脱敏后的文件毫无意义
		list, _, err := h.svc.Code.List(c.Request.Context(), repository.CodeQuery{
			ProductID: id, Status: c.Query("status"),
			Offset: offset, Limit: batch,
		}, true)
		if err != nil {
			_ = w.Write([]string{"# 导出中断: " + err.Error()})
			return
		}
		if len(list) == 0 {
			return
		}
		for _, x := range list {
			_ = w.Write([]string{
				x.Content, // List 返回的已是解密后的明文
				x.Status,
				x.OrderNo,
				utils.FormatInZone(x.CreatedAt, tz, "2006-01-02 15:04:05"),
				utils.FormatPtrInZone(x.SoldAt, tz, "2006-01-02 15:04:05"),
			})
		}
		w.Flush()
		if len(list) < batch {
			return
		}
	}
}
