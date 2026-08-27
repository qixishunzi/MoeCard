package public

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/service"
	"github.com/moecard/server/internal/utils"
)

// OrderView 是订单的前台出参。
//
// 显式构造 DTO 而不是直接返回 model.Order：
// 前台绝不应该看到 client_ip、needs_attention 这类内部字段，
// 用白名单式的 DTO 是最不容易出错的做法。
type OrderView struct {
	OrderNo    string `json:"order_no"`
	Email      string `json:"email"`
	Status     string `json:"status"`
	StatusText string `json:"status_text"`

	OriginalAmount int64  `json:"original_amount"`
	DiscountAmount int64  `json:"discount_amount"`
	PayAmount      int64  `json:"pay_amount"`
	CouponCode     string `json:"coupon_code"`

	PaymentMethod string `json:"payment_method"`
	TradeNo       string `json:"trade_no"`

	DeliveryType    string `json:"delivery_type"`
	DeliveryContent string `json:"delivery_content"`

	RefundAmount int64 `json:"refund_amount"`

	CreatedAt   string `json:"created_at"`
	PaidAt      string `json:"paid_at"`
	DeliveredAt string `json:"delivered_at"`
	ExpiredAt   string `json:"expired_at"`

	Items []OrderItemView `json:"items"`

	// CustomData 是买家自己下单时填的信息。回给他看，方便核对填错没有。
	CustomData []CustomDataView `json:"custom_data,omitempty"`
}

// CustomDataView 把存下来的 key->value 配上商品里定义的中文标签。
type CustomDataView struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
}

// OrderItemView 是订单明细的前台出参。
type OrderItemView struct {
	ProductName     string `json:"product_name"`
	ProductSlug     string `json:"product_slug"`
	ProductCover    string `json:"product_cover"`
	ProductPrice    int64  `json:"product_price"`
	Quantity        int    `json:"quantity"`
	Subtotal        int64  `json:"subtotal"`
	DeliveryType    string `json:"delivery_type"`
	DeliveryContent string `json:"delivery_content"`
}

func (h *Handler) toOrderView(o *model.Order, items []model.OrderItem) OrderView {
	tz := h.svc.Setting.Timezone()

	// 只有订单真正完成后才返回发货内容。
	// 否则 pending 状态的订单也能读到卡密，等于免费白嫖。
	revealed := o.Status == model.OrderCompleted

	v := OrderView{
		OrderNo:        o.OrderNo,
		Email:          o.Email,
		Status:         o.Status,
		StatusText:     model.OrderStatusLabel(o.Status),
		OriginalAmount: o.OriginalAmount,
		DiscountAmount: o.DiscountAmount,
		PayAmount:      o.PayAmount,
		CouponCode:     o.CouponCode,
		PaymentMethod:  o.PaymentMethod,
		TradeNo:        o.PaymentTradeNo,
		DeliveryType:   o.DeliveryType,
		RefundAmount:   o.RefundAmount,
		CreatedAt:      utils.FormatInZone(o.CreatedAt, tz, ""),
		PaidAt:         utils.FormatPtrInZone(o.PaidAt, tz, ""),
		DeliveredAt:    utils.FormatPtrInZone(o.DeliveredAt, tz, ""),
		ExpiredAt:      utils.FormatPtrInZone(o.ExpiredAt, tz, ""),
	}
	if revealed {
		v.DeliveryContent = o.DeliveryContent
	}

	v.CustomData = h.customDataOf(o, items)

	v.Items = make([]OrderItemView, 0, len(items))
	for _, it := range items {
		iv := OrderItemView{
			ProductName:  it.ProductName,
			ProductSlug:  it.ProductSlug,
			ProductCover: it.ProductCover,
			ProductPrice: it.ProductPrice,
			Quantity:     it.Quantity,
			Subtotal:     it.Subtotal,
			DeliveryType: it.DeliveryType,
		}
		if revealed {
			iv.DeliveryContent = it.DeliveryContent
		}
		v.Items = append(v.Items, iv)
	}
	return v
}

// customDataOf 把订单里的自定义信息配上商品当前的字段标签。
//
// 匹配不上的键（商品后来改过字段定义）直接用键名兜底，绝不丢值。
func (h *Handler) customDataOf(o *model.Order, items []model.OrderItem) []CustomDataView {
	if strings.TrimSpace(o.CustomData) == "" {
		return nil
	}
	var raw map[string]string
	if err := json.Unmarshal([]byte(o.CustomData), &raw); err != nil || len(raw) == 0 {
		return nil
	}

	labels := map[string]string{}
	order := make([]string, 0, len(raw))
	for _, it := range items {
		p, err := h.svc.Product.GetByID(context.Background(), it.ProductID)
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
	for k := range raw {
		if _, ok := labels[k]; !ok {
			order = append(order, k)
		}
	}

	out := make([]CustomDataView, 0, len(raw))
	for _, k := range order {
		v, ok := raw[k]
		if !ok {
			continue
		}
		label := labels[k]
		if label == "" {
			label = k
		}
		out = append(out, CustomDataView{Key: k, Label: label, Value: v})
	}
	return out
}

// CreateOrder godoc
// @Summary 创建订单
// @Router /api/v1/orders [post]
func (h *Handler) CreateOrder(c *gin.Context) {
	var in service.CreateOrderInput
	if !api.BindJSON(c, &in) {
		return
	}
	in.ClientIP = c.ClientIP()

	res, err := h.svc.Order.CreateOrder(c.Request.Context(), in)
	if err != nil {
		api.Fail(c, err)
		return
	}

	view := h.toOrderView(res.Order, res.Items)
	api.OK(c, gin.H{
		"order": view,
		// query_token 只在创建成功这一刻返回给下单者本人，
		// 之后任何列表/查询接口都不会再吐出它。
		"query_token": res.Token,
	})
}

// QueryOrder godoc
// @Summary 查询订单（订单号+邮箱 或 token）
// @Router /api/v1/orders/query [get]
func (h *Handler) QueryOrder(c *gin.Context) {
	token := strings.TrimSpace(c.Query("token"))
	ctx := c.Request.Context()

	var (
		order *model.Order
		items []model.OrderItem
		err   error
	)
	if token != "" {
		order, items, err = h.svc.Order.QueryByToken(ctx, token)
		// 同时传了订单号就必须对得上。
		// 不校验的话，order_no=A&token=B 会返回 B 的订单却带着 code=0，
		// 调用方以为拿到的是 A —— 没有越权（看到的始终是自己的单），
		// 但"要 A 给 B 还说成功"是会让人做出错误判断的响应。
		if err == nil && order != nil {
			if no := strings.TrimSpace(c.Query("order_no")); no != "" && no != order.OrderNo {
				api.FailCodef(c, api.CodeOrderNotFound, "订单号与查询凭证不匹配")
				return
			}
		}
	} else {
		orderNo := strings.TrimSpace(c.Query("order_no"))
		email := strings.TrimSpace(c.Query("email"))
		if orderNo == "" || email == "" {
			api.FailCodef(c, api.CodeBadRequest, "请提供订单号与下单邮箱")
			return
		}
		order, items, err = h.svc.Order.QueryByNoAndEmail(ctx, orderNo, email)
	}
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, h.toOrderView(order, items))
}

// GetOrderStatus godoc
// @Summary 查询订单支付状态（支付结果页轮询）
// @Router /api/v1/orders/{order_no}/status [get]
//
// 这个接口会**主动向支付平台查询**作为回调丢失时的兜底。
// 但用户看到的"支付成功"始终以后端状态为准，绝不信任前端跳转参数。
func (h *Handler) GetOrderStatus(c *gin.Context) {
	orderNo := strings.TrimSpace(c.Param("order_no"))
	if orderNo == "" {
		api.FailCode(c, api.CodeBadRequest)
		return
	}

	order, err := h.svc.Payment.SyncOrderStatus(c.Request.Context(), orderNo)
	if err != nil {
		api.Fail(c, err)
		return
	}

	// 状态接口不返回发货内容 —— 查看卡密必须走带鉴权的查询接口
	api.OK(c, gin.H{
		"order_no":    order.OrderNo,
		"status":      order.Status,
		"status_text": model.OrderStatusLabel(order.Status),
		"paid":        order.IsPaidLike(),
		"completed":   order.Status == model.OrderCompleted,
		"pay_amount":  order.PayAmount,
	})
}

// cancelOrderRequest 是取消订单的入参。
type cancelOrderRequest struct {
	Email string `json:"email" binding:"required"`
}

// CancelOrder godoc
// @Summary 取消未支付订单
// @Router /api/v1/orders/{order_no}/cancel [post]
func (h *Handler) CancelOrder(c *gin.Context) {
	orderNo := strings.TrimSpace(c.Param("order_no"))
	var req cancelOrderRequest
	if !api.BindJSON(c, &req) {
		return
	}
	if err := h.svc.Order.Cancel(c.Request.Context(), orderNo, req.Email); err != nil {
		api.Fail(c, err)
		return
	}
	api.OKMessage(c, "订单已取消", nil)
}
