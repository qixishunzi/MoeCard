package admin

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/service"
)

type couponListQuery struct {
	api.Pagination
	Keyword string `form:"keyword"`
	Type    string `form:"type"`
	Status  string `form:"status"`
	Scope   string `form:"scope"`
}

// ListCoupons godoc
// @Router /api/v1/admin/coupons [get]
func (h *Handler) ListCoupons(c *gin.Context) {
	var q couponListQuery
	if !api.BindQuery(c, &q) {
		return
	}
	q.Normalize()

	list, total, err := h.svc.Coupon.List(c.Request.Context(), repository.CouponQuery{
		Keyword: q.Keyword, Type: q.Type, Status: q.Status, Scope: q.Scope,
		Offset: q.Offset(), Limit: q.Limit(),
	})
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Page(c, list, total, q.Pagination)
}

// GetCoupon godoc
// @Router /api/v1/admin/coupons/{id} [get]
func (h *Handler) GetCoupon(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	coupon, err := h.svc.Coupon.Get(c.Request.Context(), id)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, coupon)
}

// CreateCoupon godoc
// @Router /api/v1/admin/coupons [post]
func (h *Handler) CreateCoupon(c *gin.Context) {
	var in service.CouponInput
	if !api.BindJSON(c, &in) {
		return
	}
	coupon, err := h.svc.Coupon.Create(c.Request.Context(), &in)
	if err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionCreateCoupon, "coupon", fmt.Sprint(coupon.ID), "创建优惠券: "+coupon.Code)
	api.OK(c, coupon)
}

// UpdateCoupon godoc
// @Router /api/v1/admin/coupons/{id} [put]
func (h *Handler) UpdateCoupon(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	var in service.CouponInput
	if !api.BindJSON(c, &in) {
		return
	}
	coupon, err := h.svc.Coupon.Update(c.Request.Context(), id, &in)
	if err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionUpdateCoupon, "coupon", fmt.Sprint(id), "修改优惠券: "+coupon.Code)
	api.OK(c, coupon)
}

// DeleteCoupon godoc
// @Router /api/v1/admin/coupons/{id} [delete]
func (h *Handler) DeleteCoupon(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Coupon.Delete(c.Request.Context(), id); err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionDeleteCoupon, "coupon", fmt.Sprint(id), "删除优惠券")
	api.OKMessage(c, "优惠券已删除（历史核销记录保留）", nil)
}

// ListCouponUsages godoc
// @Router /api/v1/admin/coupons/{id}/usages [get]
func (h *Handler) ListCouponUsages(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	var p api.Pagination
	if !api.BindQuery(c, &p) {
		return
	}
	p.Normalize()

	list, total, err := h.svc.Coupon.ListUsages(c.Request.Context(), id, p.Offset(), p.Limit())
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Page(c, list, total, p)
}
