// Package public 是前台（商城用户端）的 HTTP handler。
//
// 职责边界：绑定参数 → 调 service → 写响应。
// 这一层不允许出现事务、SQL 或业务分支判断。
package public

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/service"
	"github.com/moecard/server/internal/utils"
)

// Handler 持有前台所需的服务。
type Handler struct {
	svc *service.Services
}

// New 构造。
func New(svc *service.Services) *Handler { return &Handler{svc: svc} }

// ShopConfig 是前台启动时拉取的商城配置。
//
// 只包含公开信息 —— SMTP、支付密钥等绝不出现在这里。
type ShopConfig struct {
	SiteName        string `json:"site_name"`
	SiteTitle       string `json:"site_title"`
	SiteDescription string `json:"site_description"`
	SiteKeywords    string `json:"site_keywords"`
	SiteLogo        string `json:"site_logo"`
	SiteNotice      string `json:"site_notice"`
	SiteFooter      string `json:"site_footer"`
	// Contacts 是结构化的客服联系方式，链接与点击行为都由服务端算好
	Contacts []model.Contact `json:"contacts"`

	// 首页顶部轮播图。product_slug 由服务端解析好，前端直接拿去拼链接
	Banners []BannerView `json:"banners"`
	// 公告弹窗
	NoticePopup     bool   `json:"notice_popup"`
	NoticeForce     bool   `json:"notice_force_read"`
	NoticeSeconds   int    `json:"notice_force_seconds"`
	ICP             string `json:"icp"`
	Currency        string `json:"currency"`
	CurrencySymbol  string `json:"currency_symbol"`
	Timezone        string `json:"timezone"`
	AllowOrder      bool   `json:"allow_order"`
	ShowSales       bool   `json:"show_sales"`
	Maintenance     bool   `json:"maintenance"`
	MaintenanceText string `json:"maintenance_text"`
	OrderExpireMins int    `json:"order_expire_minutes"`
	Installed       bool   `json:"installed"`

	// 人机验证。SiteKey 本来就要写进页面 HTML，是公开信息；
	// 密钥（Secret Key）绝不出现在这里。
	Turnstile TurnstileConfig `json:"turnstile"`
}

// TurnstileConfig 告诉前台「要不要验证、用哪个 sitekey、哪些页面要验」。
//
// 场景开关也一并下发：前台据此决定要不要在那个页面渲染控件。
// 不下发的话前台只能每个页面都渲染一遍，用户在不需要验证的页面
// 也得等 Cloudflare 的脚本加载完。
type TurnstileConfig struct {
	Enabled bool   `json:"enabled"`
	SiteKey string `json:"site_key"`
	Size    string `json:"size"`

	OnAdminLogin bool `json:"on_admin_login"`
	OnOrder      bool `json:"on_order"`
	OnOrderQuery bool `json:"on_order_query"`
	OnCoupon     bool `json:"on_coupon"`
}

// GetConfig godoc
// @Summary 获取商城配置
// @Router /api/v1/config [get]
func (h *Handler) GetConfig(c *gin.Context) {
	s := h.svc.Setting
	api.OK(c, ShopConfig{
		SiteName:        s.SiteName(),
		SiteTitle:       s.Get(model.SetSiteTitle),
		SiteDescription: s.Get(model.SetSiteDescription),
		SiteKeywords:    s.Get(model.SetSiteKeywords),
		SiteLogo:        s.Get(model.SetSiteLogo),
		SiteNotice:      s.Get(model.SetSiteNotice),
		SiteFooter:      s.Get(model.SetSiteFooter),
		Contacts:        contactsOrEmpty(s.Contacts()),
		Banners:         h.bannersOf(c.Request.Context()),
		NoticePopup:     s.GetBool(model.SetNoticePopup),
		NoticeForce:     s.GetBool(model.SetNoticeForce),
		NoticeSeconds:   s.GetInt(model.SetNoticeForceSecond, 5),
		ICP:             s.Get(model.SetICP),
		Currency:        s.Get(model.SetCurrency),
		CurrencySymbol:  s.CurrencySymbol(),
		Timezone:        s.Timezone(),
		AllowOrder:      s.AllowOrder(),
		ShowSales:       s.ShowSales(),
		Maintenance:     s.MaintenanceMode(),
		MaintenanceText: s.Get(model.SetMaintenanceText),
		OrderExpireMins: s.GetInt(model.SetOrderExpireMinutes, 15),
		Installed:       s.IsInstalled(),
		Turnstile:       turnstileConfigOf(s),
	})
}

// BannerView 是给前台的轮播图。
type BannerView struct {
	Image string `json:"image"`
	Title string `json:"title,omitempty"`
	// ProductSlug 有值才是可点击的。商品被删或下架时它是空的 ——
	// 与其让访客点进一个 404，不如让这张图安静地只当图看
	ProductSlug string `json:"product_slug,omitempty"`
	ProductName string `json:"product_name,omitempty"`
}

// bannersOf 把已保存的轮播图配上商品链接。
//
// 一次性把用到的商品全查出来，避免每张图各查一次。
func (h *Handler) bannersOf(ctx context.Context) []BannerView {
	list := model.ParseBanners(h.svc.Setting.Get(model.SetBanners))
	if len(list) == 0 {
		return []BannerView{}
	}

	// 收集要查的商品 ID
	ids := make([]uint64, 0, len(list))
	for _, b := range list {
		if b.ProductID > 0 {
			ids = append(ids, b.ProductID)
		}
	}
	products := map[uint64]struct{ Slug, Name string }{}
	for _, id := range utils.DedupeUint64(ids) {
		p, err := h.svc.Product.GetByID(ctx, id)
		// 只有仍在售的商品才给链接：下架 / 删掉的点进去是死路
		if err != nil || p == nil || !p.IsOnSale() {
			continue
		}
		products[id] = struct{ Slug, Name string }{p.Slug, p.Name}
	}

	out := make([]BannerView, 0, len(list))
	for _, b := range list {
		v := BannerView{Image: b.Image, Title: b.Title}
		if p, ok := products[b.ProductID]; ok {
			v.ProductSlug, v.ProductName = p.Slug, p.Name
		}
		out = append(out, v)
	}
	return out
}

// contactsOrEmpty 保证出参是 []，不是 null。
//
// Go 的 nil 切片会被序列化成 null，而接口文档写的是数组 ——
// 前端 v-for 一个 null 就直接报错。空数组是这里唯一诚实的表示。
func contactsOrEmpty(list []model.Contact) []model.Contact {
	if list == nil {
		return []model.Contact{}
	}
	return list
}

// turnstileConfigOf 组装给前台的人机验证配置。
//
// 配置不完整（开了开关但少了 sitekey）时对外报「未启用」：
// 前台拿一个空 sitekey 去渲染只会得到一个报错的控件，
// 用户既过不了验证也不知道为什么 —— 那还不如当作没开。
// 真正的拦截仍由服务端中间件负责，这里的降级不会放松任何校验。
func turnstileConfigOf(s *service.SettingService) TurnstileConfig {
	ready := s.TurnstileReady()
	return TurnstileConfig{
		Enabled:      ready,
		SiteKey:      s.TurnstileSiteKey(),
		Size:         s.Get(model.SetTurnstileWidgetSize),
		OnAdminLogin: ready && s.SceneOn(model.TurnstileSceneLogin),
		OnOrder:      ready && s.SceneOn(model.TurnstileSceneOrder),
		OnOrderQuery: ready && s.SceneOn(model.TurnstileSceneQuery),
		OnCoupon:     ready && s.SceneOn(model.TurnstileSceneCoupon),
	}
}

// ListCategories godoc
// @Summary 商品分类列表
// @Router /api/v1/categories [get]
func (h *Handler) ListCategories(c *gin.Context) {
	list, err := h.svc.Category.List(c.Request.Context(), true)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, list)
}

// listProductsQuery 是商品列表的查询参数。
type listProductsQuery struct {
	api.Pagination
	CategoryID uint64 `form:"category_id"`
	Keyword    string `form:"keyword"`
	Sort       string `form:"sort"`
	Recommend  bool   `form:"recommend"`
}

// ListProducts godoc
// @Summary 商品列表
// @Router /api/v1/products [get]
func (h *Handler) ListProducts(c *gin.Context) {
	var q listProductsQuery
	if !api.BindQuery(c, &q) {
		return
	}
	q.Normalize()

	opt := service.ProductListOptions{
		ProductQuery: repository.ProductQuery{
			CategoryID: q.CategoryID,
			Keyword:    q.Keyword,
			// 前台只看得到上架商品 —— 这个约束写死在 handler，不接受客户端传参
			Status: model.ProductStatusOn,
			Sort:   q.Sort,
			Offset: q.Offset(),
			Limit:  q.Limit(),
		},
		WithStock: true,
		Public:    true,
	}
	if q.Recommend {
		t := true
		opt.Recommend = &t
	}

	list, total, err := h.svc.Product.List(c.Request.Context(), opt)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Page(c, list, total, q.Pagination)
}

// GetProduct godoc
// @Summary 商品详情
// @Router /api/v1/products/{slug} [get]
func (h *Handler) GetProduct(c *gin.Context) {
	slug := c.Param("slug")
	if slug == "" {
		api.FailCode(c, api.CodeBadRequest)
		return
	}
	p, err := h.svc.Product.GetBySlug(c.Request.Context(), slug)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, p)
}

// verifyCouponRequest 是优惠券试算的入参。
type verifyCouponRequest struct {
	Code      string `json:"code" binding:"required"`
	ProductID uint64 `json:"product_id" binding:"required"`
	Quantity  int    `json:"quantity" binding:"required,min=1"`
	Email     string `json:"email"`
}

// VerifyCoupon godoc
// @Summary 优惠券试算
// @Router /api/v1/coupons/verify [post]
func (h *Handler) VerifyCoupon(c *gin.Context) {
	var req verifyCouponRequest
	if !api.BindJSON(c, &req) {
		return
	}

	product, err := h.svc.Product.GetByID(c.Request.Context(), req.ProductID)
	if err != nil {
		api.Fail(c, err)
		return
	}
	if !product.IsOnSale() {
		api.FailCode(c, api.CodeProductOffShelf)
		return
	}

	original := product.Price * int64(req.Quantity)
	res, err := h.svc.Coupon.Validate(c.Request.Context(), nil, req.Code, req.ProductID, original, req.Email)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.OK(c, res)
}
