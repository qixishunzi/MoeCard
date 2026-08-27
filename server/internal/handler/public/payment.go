package public

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/internal/payment"
)

// ListPaymentChannels godoc
// @Summary 可用支付方式列表
// @Router /api/v1/payments [get]
func (h *Handler) ListPaymentChannels(c *gin.Context) {
	list, err := h.svc.Payment.ListPublicChannels(c.Request.Context())
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, list)
}

// payRequest 是发起支付的入参。
type payRequest struct {
	ChannelID uint64 `json:"channel_id" binding:"required"`
	Device    string `json:"device"`
}

// Pay godoc
// @Summary 发起支付
// @Router /api/v1/orders/{order_no}/pay [post]
func (h *Handler) Pay(c *gin.Context) {
	orderNo := strings.TrimSpace(c.Param("order_no"))
	var req payRequest
	if !api.BindJSON(c, &req) {
		return
	}

	device := req.Device
	if device != "mobile" && device != "pc" {
		device = detectDevice(c.GetHeader("User-Agent"))
	}

	res, err := h.svc.Payment.CreatePayment(c.Request.Context(), orderNo, req.ChannelID, device, c.ClientIP())
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, res)
}

// detectDevice 简单判断是否移动端，用于选择支付宝/微信的手机版接口。
func detectDevice(ua string) string {
	ua = strings.ToLower(ua)
	for _, kw := range []string{"mobile", "android", "iphone", "ipad", "ipod", "micromessenger", "harmony"} {
		if strings.Contains(ua, kw) {
			return "mobile"
		}
	}
	return "pc"
}

// maxNotifyBody 限制回调报文大小。正常回调都在几 KB 以内。
const maxNotifyBody = 1 << 20 // 1MB

// Notify godoc
// @Summary 支付异步回调
// @Router /api/v1/payments/notify/{provider}/{channel_id} [post]
//
// **本接口无鉴权，安全性完全依赖各渠道的验签。**
// 处理流程见 PaymentService.HandleNotify：
// 渠道匹配 → provider.VerifyNotify 验签 → OrderService.HandlePaymentSuccess。
//
// 绝不会因为报文里写着 status=success 就认为支付成功。
func (h *Handler) Notify(c *gin.Context) {
	providerKey := c.Param("provider")
	channelID, err := strconv.ParseUint(c.Param("channel_id"), 10, 64)
	if err != nil || channelID == 0 {
		logger.Payment().Warn("回调 URL 中的 channel_id 不合法",
			"provider", providerKey, "raw", c.Param("channel_id"), "ip", c.ClientIP())
		c.String(http.StatusBadRequest, "fail")
		return
	}

	// 必须读取**原始 body 字节**：微信与 Stripe 的验签都基于原始报文，
	// 任何重新序列化（哪怕只是 JSON 字段顺序变化）都会导致签名不匹配。
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxNotifyBody))
	if err != nil {
		logger.Payment().Error("读取回调请求体失败", "provider", providerKey, "err", err)
		c.String(http.StatusBadRequest, "fail")
		return
	}
	defer c.Request.Body.Close()

	contentType := c.GetHeader("Content-Type")

	// 自行解析 form，而不是用 c.Request.ParseForm() ——
	// 后者会消费 body，导致上面读到的原始字节与实际处理的不一致。
	var form url.Values
	if strings.Contains(strings.ToLower(contentType), "application/x-www-form-urlencoded") && len(body) > 0 {
		if parsed, perr := payment.ParseFormBody(body); perr == nil {
			form = parsed
		}
	}

	outcome := h.svc.Payment.HandleNotify(c.Request.Context(), providerKey, channelID, payment.NotifyRequest{
		Method:      c.Request.Method,
		Header:      c.Request.Header,
		Query:       c.Request.URL.Query(),
		Form:        form,
		Body:        body,
		ContentType: contentType,
		RemoteIP:    c.ClientIP(),
		ChannelID:   channelID,
	})

	c.Header("Content-Type", outcome.ContentType)
	c.String(outcome.StatusCode, outcome.Body)
}

// Return godoc
// @Summary 支付同步跳转
// @Router /api/v1/payments/return/{provider}/{channel_id} [get]
//
// 这个接口**只负责把用户带回前端结果页**，不做任何支付状态判定。
// 真正的支付状态以异步回调 / 主动查询为准 ——
// 同步跳转的参数完全由用户浏览器携带，可以随意伪造。
func (h *Handler) Return(c *gin.Context) {
	orderNo := c.Query("out_trade_no")
	for _, k := range []string{"order_no", "client_reference_id", "merchant_order_no"} {
		if orderNo != "" {
			break
		}
		orderNo = c.Query(k)
	}

	target := h.svc.Payment.ReturnURL(orderNo)
	c.Redirect(http.StatusFound, target)
}
