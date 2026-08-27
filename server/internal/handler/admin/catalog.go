package admin

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/service"
)

// ---- 分类 ----

// ListCategories godoc
// @Router /api/v1/admin/categories [get]
func (h *Handler) ListCategories(c *gin.Context) {
	list, err := h.svc.Category.List(c.Request.Context(), false)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, list)
}

// GetCategory godoc
// @Router /api/v1/admin/categories/{id} [get]
func (h *Handler) GetCategory(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	cat, err := h.svc.Category.Get(c.Request.Context(), id)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, cat)
}

// CreateCategory godoc
// @Router /api/v1/admin/categories [post]
func (h *Handler) CreateCategory(c *gin.Context) {
	var in service.CategoryInput
	if !api.BindJSON(c, &in) {
		return
	}
	cat, err := h.svc.Category.Create(c.Request.Context(), &in)
	if err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionCreateCategory, "category", fmt.Sprint(cat.ID), "创建分类: "+cat.Name)
	api.OK(c, cat)
}

// UpdateCategory godoc
// @Router /api/v1/admin/categories/{id} [put]
func (h *Handler) UpdateCategory(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	var in service.CategoryInput
	if !api.BindJSON(c, &in) {
		return
	}
	cat, err := h.svc.Category.Update(c.Request.Context(), id, &in)
	if err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionUpdateCategory, "category", fmt.Sprint(id), "修改分类: "+cat.Name)
	api.OK(c, cat)
}

// DeleteCategory godoc
// @Router /api/v1/admin/categories/{id} [delete]
func (h *Handler) DeleteCategory(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Category.Delete(c.Request.Context(), id); err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionDeleteCategory, "category", fmt.Sprint(id), "删除分类")
	api.OKMessage(c, "分类已删除", nil)
}

type moveProductsRequest struct {
	TargetCategoryID uint64 `json:"target_category_id" binding:"required"`
}

// MoveCategoryProducts godoc
// @Router /api/v1/admin/categories/{id}/move [post]
//
// 配合"分类下有商品不允许删除"的保护：先转移，再删除。
func (h *Handler) MoveCategoryProducts(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	var req moveProductsRequest
	if !api.BindJSON(c, &req) {
		return
	}
	if req.TargetCategoryID == id {
		api.FailCodef(c, api.CodeValidation, "目标分类不能是当前分类")
		return
	}
	n, err := h.svc.Category.MoveProducts(c.Request.Context(), id, req.TargetCategoryID)
	if err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionUpdateCategory, "category", fmt.Sprint(id),
		fmt.Sprintf("转移 %d 个商品到分类 #%d", n, req.TargetCategoryID))
	api.OKMessage(c, fmt.Sprintf("已转移 %d 个商品", n), gin.H{"moved": n})
}

// ---- 商品 ----

type adminProductQuery struct {
	api.Pagination
	CategoryID   uint64 `form:"category_id"`
	Keyword      string `form:"keyword"`
	Status       string `form:"status"`
	DeliveryType string `form:"delivery_type"`
	Sort         string `form:"sort"`
}

// ListProducts godoc
// @Router /api/v1/admin/products [get]
func (h *Handler) ListProducts(c *gin.Context) {
	var q adminProductQuery
	if !api.BindQuery(c, &q) {
		return
	}
	q.Normalize()

	list, total, err := h.svc.Product.List(c.Request.Context(), service.ProductListOptions{
		ProductQuery: repository.ProductQuery{
			CategoryID:   q.CategoryID,
			Keyword:      q.Keyword,
			Status:       q.Status,
			DeliveryType: q.DeliveryType,
			Sort:         q.Sort,
			Offset:       q.Offset(),
			Limit:        q.Limit(),
		},
		WithStock: true,
	})
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Page(c, list, total, q.Pagination)
}

// GetProduct godoc
// @Router /api/v1/admin/products/{id} [get]
func (h *Handler) GetProduct(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	p, err := h.svc.Product.GetByID(c.Request.Context(), id)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, p)
}

// CreateProduct godoc
// @Router /api/v1/admin/products [post]
func (h *Handler) CreateProduct(c *gin.Context) {
	var in service.ProductInput
	if !api.BindJSON(c, &in) {
		return
	}
	p, err := h.svc.Product.Create(c.Request.Context(), &in)
	if err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionCreateProduct, "product", fmt.Sprint(p.ID), "创建商品: "+p.Name)
	api.OK(c, p)
}

// UpdateProduct godoc
// @Router /api/v1/admin/products/{id} [put]
func (h *Handler) UpdateProduct(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	var in service.ProductInput
	if !api.BindJSON(c, &in) {
		return
	}
	p, err := h.svc.Product.Update(c.Request.Context(), id, &in)
	if err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionUpdateProduct, "product", fmt.Sprint(id), "修改商品: "+p.Name)
	api.OK(c, p)
}

type productStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=on off"`
}

// SetProductStatus godoc
// @Router /api/v1/admin/products/{id}/status [post]
func (h *Handler) SetProductStatus(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	var req productStatusRequest
	if !api.BindJSON(c, &req) {
		return
	}
	if err := h.svc.Product.SetStatus(c.Request.Context(), id, req.Status); err != nil {
		api.Fail(c, err)
		return
	}
	label := "上架"
	if req.Status == model.ProductStatusOff {
		label = "下架"
	}
	h.log(c, model.ActionUpdateProduct, "product", fmt.Sprint(id), label+"商品")
	api.OKMessage(c, "商品已"+label, nil)
}

type productStockRequest struct {
	Stock int64 `json:"stock"`
}

// SetProductStock godoc
// @Router /api/v1/admin/products/{id}/stock [post]
func (h *Handler) SetProductStock(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	var req productStockRequest
	if !api.BindJSON(c, &req) {
		return
	}
	if err := h.svc.Product.UpdateStock(c.Request.Context(), id, req.Stock); err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionUpdateProduct, "product", fmt.Sprint(id),
		fmt.Sprintf("修改库存为 %d", req.Stock))
	api.OKMessage(c, "库存已更新", nil)
}

// DeleteProduct godoc
// @Router /api/v1/admin/products/{id} [delete]
func (h *Handler) DeleteProduct(c *gin.Context) {
	id, ok := api.ParamUint(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Product.Delete(c.Request.Context(), id); err != nil {
		api.Fail(c, err)
		return
	}
	h.log(c, model.ActionDeleteProduct, "product", fmt.Sprint(id), "删除商品")
	api.OKMessage(c, "商品已删除（历史订单不受影响）", nil)
}

// Upload godoc
// @Router /api/v1/admin/upload [post]
//
// 安全措施全部在 storage.LocalStorage.Save 内实现：
// 大小限制、真实 MIME 嗅探、服务端生成文件名与路径。
func (h *Handler) Upload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		api.FailCodef(c, api.CodeUploadInvalid, "请选择要上传的文件")
		return
	}
	url, err := h.svc.Storage.Save(file)
	if err != nil {
		api.FailCodef(c, api.CodeUploadBadFormat, "%s", err.Error())
		return
	}
	h.log(c, model.ActionUploadFile, "file", url, "上传文件")
	api.OK(c, gin.H{"url": url})
}
