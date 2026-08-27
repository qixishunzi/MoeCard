package admin

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/repository"
)

type codeListQuery struct {
	api.Pagination
	Status  string `form:"status"`
	Keyword string `form:"keyword"`
	OrderNo string `form:"order_no"`
	Reveal  bool   `form:"reveal"`
}

// ListCodes godoc
// @Router /api/v1/admin/products/{id}/codes [get]
//
// 默认返回脱敏的卡密内容；显式传 reveal=1 才返回明文。
// 这样后台列表被截图/投屏时不会直接泄露全部卡密。
func (h *Handler) ListCodes(c *gin.Context) {
	productID, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	var q codeListQuery
	if !api.BindQuery(c, &q) {
		return
	}
	q.Normalize()

	list, total, err := h.svc.Code.List(c.Request.Context(), repository.CodeQuery{
		ProductID: productID,
		Status:    q.Status,
		Keyword:   q.Keyword,
		OrderNo:   q.OrderNo,
		Offset:    q.Offset(),
		Limit:     q.Limit(),
	}, q.Reveal)
	if err != nil {
		api.Fail(c, err)
		return
	}
	if q.Reveal {
		h.log(c, model.ActionImportCodes, "product", fmt.Sprint(productID), "查看卡密明文")
	}
	api.Page(c, list, total, q.Pagination)
}

// CodeStats godoc
// @Router /api/v1/admin/products/{id}/codes/stats [get]
func (h *Handler) CodeStats(c *gin.Context) {
	productID, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	stats, err := h.svc.Code.Stats(c.Request.Context(), productID)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, stats)
}

type importCodesRequest struct {
	Content string `json:"content" binding:"required"`
}

// ImportCodes godoc
// @Router /api/v1/admin/products/{id}/codes [post]
//
// 一行一个卡密。自动 trim 空格、忽略空行、批内去重、
// 与数据库已有卡密去重（(product_id, content_hash) 唯一索引兜底）。
func (h *Handler) ImportCodes(c *gin.Context) {
	productID, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	var req importCodesRequest
	if !api.BindJSON(c, &req) {
		return
	}

	res, err := h.svc.Code.Import(c.Request.Context(), productID, req.Content)
	if err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionImportCodes, "product", fmt.Sprint(productID),
		fmt.Sprintf("导入卡密：提交 %d 条，成功 %d 条，重复跳过 %d 条",
			res.Total, res.Imported, res.Duplicate))

	msg := fmt.Sprintf("成功导入 %d 条卡密", res.Imported)
	if res.Duplicate > 0 {
		msg += fmt.Sprintf("，跳过 %d 条重复卡密", res.Duplicate)
	}
	api.OKMessage(c, msg, res)
}

type deleteCodesRequest struct {
	IDs       []uint64 `json:"ids"`
	AllUnused bool     `json:"all_unused"`
}

// DeleteCodes godoc
// @Router /api/v1/admin/products/{id}/codes [delete]
//
// 只能删除未使用的卡密。已锁定/已售出的卡密关联着真实订单，
// 删掉会让买家在订单详情里看不到自己买的东西。
func (h *Handler) DeleteCodes(c *gin.Context) {
	productID, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	var req deleteCodesRequest
	if !api.BindJSON(c, &req) {
		return
	}

	n, err := h.svc.Code.DeleteUnused(c.Request.Context(), productID, req.IDs, req.AllUnused)
	if err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionDeleteCodes, "product", fmt.Sprint(productID),
		fmt.Sprintf("删除 %d 条未使用卡密", n))
	api.OKMessage(c, fmt.Sprintf("已删除 %d 条卡密", n), gin.H{"deleted": n})
}

// DeleteCode godoc
// @Router /api/v1/admin/codes/{id} [delete]
func (h *Handler) DeleteCode(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Code.DeleteOne(c.Request.Context(), id); err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionDeleteCodes, "code", fmt.Sprint(id), "删除单条卡密")
	api.OKMessage(c, "卡密已删除", nil)
}

// ---- 卡密总览（跨商品）----
//
// 商品详情里的卡密抽屉一次只能看一个商品。真正的日常管理是
// "全站还剩多少卡密""这条卡密卖给了谁""哪个商品快没货了"，
// 这些问题在单商品视图里都答不了，所以单独开一组不绑定商品的接口。

type globalCodeListQuery struct {
	api.Pagination
	ProductID uint64 `form:"product_id"`
	Status    string `form:"status"`
	Keyword   string `form:"keyword"`
	OrderNo   string `form:"order_no"`
	Reveal    bool   `form:"reveal"`
}

// ListAllCodes godoc
// @Summary 卡密总览列表（可选按商品过滤）
// @Router /api/v1/admin/codes [get]
func (h *Handler) ListAllCodes(c *gin.Context) {
	var q globalCodeListQuery
	if !api.BindQuery(c, &q) {
		return
	}
	q.Normalize()

	list, total, err := h.svc.Code.List(c.Request.Context(), repository.CodeQuery{
		ProductID: q.ProductID,
		Status:    q.Status,
		Keyword:   q.Keyword,
		OrderNo:   q.OrderNo,
		Offset:    q.Offset(),
		Limit:     q.Limit(),
	}, q.Reveal)
	if err != nil {
		api.Fail(c, err)
		return
	}
	if q.Reveal {
		h.log(c, model.ActionImportCodes, "code", "", "在卡密总览查看明文")
	}
	api.Page(c, list, total, q.Pagination)
}

// AllCodeStats godoc
// @Summary 全站卡密状态统计
// @Router /api/v1/admin/codes/stats [get]
func (h *Handler) AllCodeStats(c *gin.Context) {
	stats, err := h.svc.Code.StatsGlobal(c.Request.Context())
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, stats)
}

// CodeInventory godoc
// @Summary 各商品卡密库存分布
// @Router /api/v1/admin/codes/inventory [get]
func (h *Handler) CodeInventory(c *gin.Context) {
	rows, err := h.svc.Code.StockOverview(c.Request.Context())
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, rows)
}

type importAnyCodesRequest struct {
	ProductID uint64 `json:"product_id" binding:"required"`
	Content   string `json:"content" binding:"required"`
}

// ImportAnyCodes godoc
// @Summary 在总览页导入卡密（商品从下拉选择）
// @Router /api/v1/admin/codes [post]
func (h *Handler) ImportAnyCodes(c *gin.Context) {
	var req importAnyCodesRequest
	if !api.BindJSON(c, &req) {
		return
	}

	res, err := h.svc.Code.Import(c.Request.Context(), req.ProductID, req.Content)
	if err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionImportCodes, "product", fmt.Sprint(req.ProductID),
		fmt.Sprintf("导入卡密：提交 %d 条，成功 %d 条，重复跳过 %d 条",
			res.Total, res.Imported, res.Duplicate))

	msg := fmt.Sprintf("成功导入 %d 条卡密", res.Imported)
	if res.Duplicate > 0 {
		msg += fmt.Sprintf("，跳过 %d 条重复卡密", res.Duplicate)
	}
	api.OKMessage(c, msg, res)
}

type deleteAnyCodesRequest struct {
	IDs []uint64 `json:"ids" binding:"required"`
}

// DeleteAnyCodes godoc
// @Summary 跨商品批量删除未使用卡密
// @Router /api/v1/admin/codes [delete]
func (h *Handler) DeleteAnyCodes(c *gin.Context) {
	var req deleteAnyCodesRequest
	if !api.BindJSON(c, &req) {
		return
	}
	n, err := h.svc.Code.DeleteByIDs(c.Request.Context(), req.IDs)
	if err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionDeleteCodes, "code", "",
		fmt.Sprintf("在卡密总览删除 %d 条未使用卡密", n))
	api.OKMessage(c, fmt.Sprintf("已删除 %d 条卡密", n), gin.H{"deleted": n})
}
