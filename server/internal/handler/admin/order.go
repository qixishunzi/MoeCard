package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/service"
	"github.com/moecard/server/internal/utils"
)

type adminOrderQuery struct {
	api.Pagination
	Keyword        string `form:"keyword"`
	OrderNo        string `form:"order_no"`
	Email          string `form:"email"`
	ProductKeyword string `form:"product"`
	ProductID      uint64 `form:"product_id"`
	Status         string `form:"status"`
	Provider       string `form:"provider"`
	ChannelID      uint64 `form:"channel_id"`
	Attention      string `form:"attention"`
	StartAt        string `form:"start_at"`
	EndAt          string `form:"end_at"`
}

// ListOrders godoc
// @Router /api/v1/admin/orders [get]
func (h *Handler) ListOrders(c *gin.Context) {
	var q adminOrderQuery
	if !api.BindQuery(c, &q) {
		return
	}
	q.Normalize()

	tz := h.svc.Setting.Timezone()
	start, err := service.ParseAdminTime(q.StartAt, tz)
	if err != nil {
		api.FailCodef(c, api.CodeValidation, "开始时间格式不正确")
		return
	}
	end, err := service.ParseAdminTime(q.EndAt, tz)
	if err != nil {
		api.FailCodef(c, api.CodeValidation, "结束时间格式不正确")
		return
	}
	// 结束日期按"当天结束"处理，否则筛 2026-08-26 会漏掉当天的订单
	if end != nil && len(strings.TrimSpace(q.EndAt)) == 10 {
		t := end.AddDate(0, 0, 1)
		end = &t
	}

	rq := repository.OrderQuery{
		Keyword:        q.Keyword,
		OrderNo:        q.OrderNo,
		Email:          q.Email,
		ProductID:      q.ProductID,
		ProductKeyword: q.ProductKeyword,
		Status:         q.Status,
		Provider:       q.Provider,
		ChannelID:      q.ChannelID,
		StartAt:        start,
		EndAt:          end,
		Offset:         q.Offset(),
		Limit:          q.Limit(),
	}
	if q.Attention == "1" {
		t := true
		rq.NeedsAttention = &t
	}

	list, total, err := h.svc.Order.List(c.Request.Context(), rq)
	if err != nil {
		api.Fail(c, err)
		return
	}

	views := make([]adminOrderView, 0, len(list))
	for i := range list {
		views = append(views, h.toAdminOrderView(c.Request.Context(), &list[i], list[i].Items, false))
	}
	api.Page(c, views, total, q.Pagination)
}

// adminOrderView 是后台订单出参。
//
// 列表默认不返回发货内容（卡密）—— 那是最敏感的数据，
// 只有点进详情才返回，减少批量泄露的面。
type adminOrderView struct {
	ID         uint64 `json:"id"`
	OrderNo    string `json:"order_no"`
	Email      string `json:"email"`
	Status     string `json:"status"`
	StatusText string `json:"status_text"`

	OriginalAmount int64  `json:"original_amount"`
	DiscountAmount int64  `json:"discount_amount"`
	PayAmount      int64  `json:"pay_amount"`
	CouponCode     string `json:"coupon_code"`

	PaymentMethod   string `json:"payment_method"`
	PaymentProvider string `json:"payment_provider"`
	TradeNo         string `json:"trade_no"`

	DeliveryType    string `json:"delivery_type"`
	DeliveryContent string `json:"delivery_content,omitempty"`

	NeedsAttention  bool   `json:"needs_attention"`
	AttentionReason string `json:"attention_reason"`
	Remark          string `json:"remark"`
	ClientIP        string `json:"client_ip,omitempty"`

	RefundAmount int64  `json:"refund_amount"`
	RefundReason string `json:"refund_reason"`
	RefundedAt   string `json:"refunded_at"`

	CreatedAt   string `json:"created_at"`
	PaidAt      string `json:"paid_at"`
	DeliveredAt string `json:"delivered_at"`
	ExpiredAt   string `json:"expired_at"`

	Items []adminOrderItemView `json:"items"`

	// CustomData 是买家下单时按商品要求填写的信息（游戏账号、大区等）。
	// 手动发货商品全靠它才知道该把东西发给谁，只在详情里返回。
	CustomData []customDataView `json:"custom_data,omitempty"`
}

// customDataView 把存下来的 key->value 还原成人看得懂的「标签 + 值」。
// 只回 key 的话后台显示的就是 account、uid 这种内部标识，
// 而管理员在商品里配置时填的是「接收账号」。
type customDataView struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
}

type adminOrderItemView struct {
	ID              uint64 `json:"id"`
	ProductID       uint64 `json:"product_id"`
	ProductName     string `json:"product_name"`
	ProductCover    string `json:"product_cover"`
	ProductPrice    int64  `json:"product_price"`
	Quantity        int    `json:"quantity"`
	Subtotal        int64  `json:"subtotal"`
	DeliveryType    string `json:"delivery_type"`
	DeliveryContent string `json:"delivery_content,omitempty"`
}

func (h *Handler) toAdminOrderView(ctx context.Context, o *model.Order, items []model.OrderItem, detail bool) adminOrderView {
	tz := h.svc.Setting.Timezone()
	v := adminOrderView{
		ID:              o.ID,
		OrderNo:         o.OrderNo,
		Email:           o.Email,
		Status:          o.Status,
		StatusText:      model.OrderStatusLabel(o.Status),
		OriginalAmount:  o.OriginalAmount,
		DiscountAmount:  o.DiscountAmount,
		PayAmount:       o.PayAmount,
		CouponCode:      o.CouponCode,
		PaymentMethod:   o.PaymentMethod,
		PaymentProvider: o.PaymentProvider,
		TradeNo:         o.PaymentTradeNo,
		DeliveryType:    o.DeliveryType,
		NeedsAttention:  o.NeedsAttention,
		AttentionReason: o.AttentionReason,
		Remark:          o.Remark,
		RefundAmount:    o.RefundAmount,
		RefundReason:    o.RefundReason,
		RefundedAt:      utils.FormatPtrInZone(o.RefundedAt, tz, ""),
		CreatedAt:       utils.FormatInZone(o.CreatedAt, tz, ""),
		PaidAt:          utils.FormatPtrInZone(o.PaidAt, tz, ""),
		DeliveredAt:     utils.FormatPtrInZone(o.DeliveredAt, tz, ""),
		ExpiredAt:       utils.FormatPtrInZone(o.ExpiredAt, tz, ""),
	}
	if detail {
		v.DeliveryContent = o.DeliveryContent
		v.ClientIP = o.ClientIP
		v.CustomData = h.customDataOf(ctx, o, items)
	} else {
		// 列表页对邮箱脱敏：后台截图、投屏、共享时不至于泄露全部买家邮箱
		v.Email = utils.MaskEmail(o.Email)
	}

	v.Items = make([]adminOrderItemView, 0, len(items))
	for _, it := range items {
		iv := adminOrderItemView{
			ID: it.ID, ProductID: it.ProductID,
			ProductName: it.ProductName, ProductCover: it.ProductCover,
			ProductPrice: it.ProductPrice, Quantity: it.Quantity,
			Subtotal: it.Subtotal, DeliveryType: it.DeliveryType,
		}
		if detail {
			iv.DeliveryContent = it.DeliveryContent
		}
		v.Items = append(v.Items, iv)
	}
	return v
}

// customDataOf 把订单里的自定义信息配上商品当时的字段标签。
//
// 标签取自商品**当前**的字段定义；商品改过字段名的话，
// 匹配不上的键就直接用键名兜底，绝不因为对不上就把值丢掉 ——
// 那可是买家填的收货账号，丢了这单就发不出去了。
func (h *Handler) customDataOf(ctx context.Context, o *model.Order, items []model.OrderItem) []customDataView {
	if strings.TrimSpace(o.CustomData) == "" {
		return nil
	}
	var raw map[string]string
	if err := json.Unmarshal([]byte(o.CustomData), &raw); err != nil || len(raw) == 0 {
		return nil
	}

	labels := map[string]string{}
	order := []string{}
	for _, it := range items {
		p, err := h.svc.Product.GetByID(ctx, it.ProductID)
		if err != nil || p == nil {
			continue
		}
		for _, f := range service.ParseCustomFields(p.CustomFields) {
			if _, seen := labels[f.Key]; !seen {
				labels[f.Key] = f.Label
				order = append(order, f.Key)
			}
		}
	}
	// 商品里定义过的字段按定义顺序排，剩下的（字段被删过）追加在后面
	for k := range raw {
		if _, ok := labels[k]; !ok {
			order = append(order, k)
		}
	}

	out := make([]customDataView, 0, len(raw))
	for _, k := range order {
		v, ok := raw[k]
		if !ok {
			continue
		}
		label := labels[k]
		if label == "" {
			label = k
		}
		out = append(out, customDataView{Key: k, Label: label, Value: v})
	}
	return out
}

// GetOrder godoc
// @Router /api/v1/admin/orders/{id} [get]
func (h *Handler) GetOrder(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	order, items, err := h.svc.Order.GetByID(c.Request.Context(), id)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, h.toAdminOrderView(c.Request.Context(), order, items, true))
}

type deliverRequest struct {
	Content string `json:"content" binding:"required"`
}

// DeliverOrder godoc
// @Router /api/v1/admin/orders/{id}/deliver [post]
//
// 手动发货：写入发货内容 → 订单转 completed → 异步给买家发邮件。
func (h *Handler) DeliverOrder(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	var req deliverRequest
	if !api.BindJSON(c, &req) {
		return
	}
	order, items, err := h.svc.Order.ManualDeliver(c.Request.Context(), id, req.Content)
	if err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionDeliverOrder, "order", order.OrderNo, "手动发货")
	api.OKMessage(c, "发货成功，已通知买家", h.toAdminOrderView(c.Request.Context(), order, items, true))
}

type remarkRequest struct {
	Remark string `json:"remark"`
}

// RemarkOrder godoc
// @Router /api/v1/admin/orders/{id}/remark [post]
func (h *Handler) RemarkOrder(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	var req remarkRequest
	if !api.BindJSON(c, &req) {
		return
	}
	if err := h.svc.Order.AddRemark(c.Request.Context(), id, req.Remark); err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionRemarkOrder, "order", fmt.Sprint(id), "修改订单备注")
	api.OKMessage(c, "备注已保存", nil)
}

// RefundOrder godoc
// @Router /api/v1/admin/orders/{id}/refund [post]
//
// manual=true 时只记账（适用于渠道不支持自动退款的场景），
// 否则调用支付渠道的退款接口。无论哪种方式，订单都会完整记录
// 退款金额、时间与原因。
func (h *Handler) RefundOrder(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	var in service.RefundInput
	if !api.BindJSON(c, &in) {
		return
	}
	order, err := h.svc.Payment.Refund(c.Request.Context(), id, in)
	if err != nil {
		api.Fail(c, err)
		return
	}
	mode := "渠道退款"
	if in.Manual {
		mode = "人工退款"
	}
	h.log(c, model.ActionRefundOrder, "order", order.OrderNo,
		fmt.Sprintf("%s %s，原因：%s", mode, utils.FormatAmount(order.RefundAmount), in.Reason))
	api.OKMessage(c, "退款已处理", nil)
}

// ClearOrderAttention godoc
// @Router /api/v1/admin/orders/{id}/attention [delete]
func (h *Handler) ClearOrderAttention(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Order.ClearAttention(c.Request.Context(), id); err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionRemarkOrder, "order", fmt.Sprint(id), "清除异常标记")
	api.OKMessage(c, "已清除异常标记", nil)
}

type resendMailRequest struct {
	Template string `json:"template"`
}

// ResendOrderMail godoc
// @Router /api/v1/admin/orders/{id}/resend-mail [post]
func (h *Handler) ResendOrderMail(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	var req resendMailRequest
	_ = c.ShouldBindJSON(&req)

	order, items, err := h.svc.Order.GetByID(c.Request.Context(), id)
	if err != nil {
		api.Fail(c, err)
		return
	}

	tpl := req.Template
	if tpl == "" {
		switch {
		case order.Status == model.OrderCompleted && order.DeliveryType == model.DeliveryAuto:
			tpl = model.MailTemplateDeliver
		case order.Status == model.OrderCompleted:
			tpl = model.MailTemplateManual
		default:
			tpl = model.MailTemplatePaid
		}
	}

	if err := h.svc.Mail.SendOrderMailSync(c.Request.Context(), order, items, tpl); err != nil {
		api.FailCodef(c, api.CodeMailSendFailed, "邮件发送失败: %s", err.Error())
		return
	}
	h.log(c, model.ActionResendMail, "order", order.OrderNo, "重新发送邮件: "+tpl)
	api.OKMessage(c, "邮件已发送", nil)
}
