package admin

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/service"
)

// ListPaymentProviders godoc
// @Router /api/v1/admin/payments/providers [get]
//
// 返回全部已注册 provider 的配置字段描述，前端据此动态渲染表单。
// 新增支付渠道时前端无需改代码。
func (h *Handler) ListPaymentProviders(c *gin.Context) {
	api.OK(c, h.svc.Payment.Descriptors())
}

// ListPaymentChannels godoc
// @Router /api/v1/admin/payments [get]
func (h *Handler) ListPaymentChannels(c *gin.Context) {
	list, err := h.svc.Payment.ListAdminChannels(c.Request.Context())
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, list)
}

// GetPaymentChannel godoc
// @Router /api/v1/admin/payments/{id} [get]
func (h *Handler) GetPaymentChannel(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	ch, err := h.svc.Payment.GetAdminChannel(c.Request.Context(), id)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, ch)
}

// CreatePaymentChannel godoc
// @Router /api/v1/admin/payments [post]
func (h *Handler) CreatePaymentChannel(c *gin.Context) {
	var in service.ChannelInput
	if !api.BindJSON(c, &in) {
		return
	}
	ch, err := h.svc.Payment.CreateChannel(c.Request.Context(), &in)
	if err != nil {
		api.Fail(c, err)
		return
	}
	// 日志里只记渠道名与 provider，绝不记 config —— 那里面有密钥
	h.log(c, model.ActionCreateChannel, "payment_channel", fmt.Sprint(ch.ID),
		fmt.Sprintf("创建支付渠道: %s (%s)", ch.Name, ch.Provider))
	api.OK(c, ch)
}

// UpdatePaymentChannel godoc
// @Router /api/v1/admin/payments/{id} [put]
//
// 敏感字段（密钥/私钥）若提交的是脱敏值，会保留数据库中的旧值。
// 这条规则实现在 service.mergeSecrets —— 没有它，管理员改个排序
// 就会把支付密钥覆盖成一串星号。
func (h *Handler) UpdatePaymentChannel(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	var in service.ChannelInput
	if !api.BindJSON(c, &in) {
		return
	}
	ch, err := h.svc.Payment.UpdateChannel(c.Request.Context(), id, &in)
	if err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionUpdateChannel, "payment_channel", fmt.Sprint(id),
		fmt.Sprintf("修改支付渠道: %s (%s)，状态: %s", ch.Name, ch.Provider, ch.Status))
	api.OK(c, ch)
}

// DeletePaymentChannel godoc
// @Router /api/v1/admin/payments/{id} [delete]
func (h *Handler) DeletePaymentChannel(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Payment.DeleteChannel(c.Request.Context(), id); err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionDeleteChannel, "payment_channel", fmt.Sprint(id), "删除支付渠道")
	api.OKMessage(c, "支付渠道已删除", nil)
}

// TestPaymentChannel godoc
// @Router /api/v1/admin/payments/{id}/test [post]
//
// 通过创建一笔 1 分钱的测试支付单验证配置。
// 只创建、不支付，不会产生任何真实扣款。
func (h *Handler) TestPaymentChannel(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	res, err := h.svc.Payment.TestChannel(c.Request.Context(), id)
	if err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionUpdateChannel, "payment_channel", fmt.Sprint(id), "测试支付渠道配置")
	api.OK(c, res)
}

type paymentLogQuery struct {
	api.Pagination
	OrderNo  string `form:"order_no"`
	Provider string `form:"provider"`
	Event    string `form:"event"`
}

// ListPaymentLogs godoc
// @Router /api/v1/admin/logs/payments [get]
func (h *Handler) ListPaymentLogs(c *gin.Context) {
	var q paymentLogQuery
	if !api.BindQuery(c, &q) {
		return
	}
	q.Normalize()

	list, total, err := h.svc.Payment.ListLogs(c.Request.Context(), repository.PaymentLogQuery{
		OrderNo: q.OrderNo, Provider: q.Provider, Event: q.Event,
		Offset: q.Offset(), Limit: q.Limit(),
	})
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Page(c, list, total, q.Pagination)
}
