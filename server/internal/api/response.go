package api

import (
	"log/slog"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/moecard/server/internal/logger"
)

// Version 是当前 API 版本号。
const Version = "1.0.0"

// BuildInfo 是这份二进制的身份：线上排查时"你跑的是哪个包"是第一个问题。
//
// 放在 api 包而不是 router：router 引用 handler，handler 再引用 router
// 就成环了，而两边都已经引用 api。
type BuildInfo struct {
	Version   string `json:"version"`
	BuildTime string `json:"build_time"`
	Commit    string `json:"commit"`
}

// Response 是全站统一响应体。
type Response struct {
	Code    ErrCode `json:"code"`
	Message string  `json:"message"`
	Data    any     `json:"data"`
}

// PageData 是统一分页结构。
type PageData struct {
	List     any   `json:"list"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Total    int64 `json:"total"`
}

// Pagination 是列表接口的通用查询参数。
type Pagination struct {
	Page     int `form:"page" json:"page"`
	PageSize int `form:"page_size" json:"page_size"`
}

// Normalize 收敛分页参数，防止 page_size 过大拖垮数据库。
func (p *Pagination) Normalize() {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PageSize < 1 {
		p.PageSize = 20
	}
	if p.PageSize > 100 {
		p.PageSize = 100
	}
}

// Offset 返回 SQL OFFSET。
func (p Pagination) Offset() int { return (p.Page - 1) * p.PageSize }

// Limit 返回 SQL LIMIT。
func (p Pagination) Limit() int { return p.PageSize }

// OK 返回成功响应。
func OK(c *gin.Context, data any) {
	if data == nil {
		data = struct{}{}
	}
	c.JSON(200, Response{Code: CodeSuccess, Message: "success", Data: data})
}

// OKMessage 返回带自定义文案的成功响应。
func OKMessage(c *gin.Context, message string, data any) {
	if data == nil {
		data = struct{}{}
	}
	c.JSON(200, Response{Code: CodeSuccess, Message: message, Data: data})
}

// Page 返回分页响应。list 为 nil 时用空数组代替，保证前端拿到的永远是数组。
func Page(c *gin.Context, list any, total int64, p Pagination) {
	if list == nil {
		list = []any{}
	}
	OK(c, PageData{List: list, Page: p.Page, PageSize: p.PageSize, Total: total})
}

// Fail 把 error 转换为统一响应。内部错误只记日志，不外泄细节。
func Fail(c *gin.Context, err error) {
	e := AsError(err)
	if e == nil {
		OK(c, nil)
		return
	}

	if e.Err != nil {
		attrs := []any{
			"code", int(e.Code),
			"path", c.Request.URL.Path,
			"method", c.Request.Method,
			"err", e.Err.Error(),
		}
		if rid := c.GetString(string(logger.RequestIDKey)); rid != "" {
			attrs = append(attrs, "request_id", rid)
		}
		if e.Code >= 50000 {
			logger.L().Error("request failed", attrs...)
		} else {
			logger.L().Warn("request rejected", attrs...)
		}
	}

	msg := e.Message
	if msg == "" {
		msg = e.Code.Message()
	}
	c.AbortWithStatusJSON(httpStatusOf(e.Code), Response{Code: e.Code, Message: msg, Data: nil})
}

// FailCode 用错误码直接失败。
func FailCode(c *gin.Context, code ErrCode) { Fail(c, NewError(code)) }

// FailCodef 用错误码 + 自定义文案失败。
func FailCodef(c *gin.Context, code ErrCode, format string, args ...any) {
	Fail(c, NewErrorf(code, format, args...))
}

// BindJSON 绑定并校验 JSON 请求体，失败时自动写响应并返回 false。
func BindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		Fail(c, WrapErrorf(CodeValidation, err, "参数校验失败: %s", err.Error()))
		return false
	}
	return true
}

// BindQuery 绑定并校验 query 参数。
func BindQuery(c *gin.Context, dst any) bool {
	if err := c.ShouldBindQuery(dst); err != nil {
		Fail(c, WrapErrorf(CodeValidation, err, "参数校验失败: %s", err.Error()))
		return false
	}
	return true
}

// ParamUint 从路径参数解析正整数 ID。
func ParamUint(c *gin.Context, name string) (uint64, bool) {
	raw := c.Param(name)
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || v == 0 {
		FailCodef(c, CodeBadRequest, "路径参数 %s 不合法", name)
		return 0, false
	}
	return v, true
}

// LogAttr 便捷构造日志字段。
func LogAttr(key string, val any) slog.Attr { return slog.Any(key, val) }
