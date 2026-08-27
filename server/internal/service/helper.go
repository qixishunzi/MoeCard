package service

import (
	"errors"
	"strings"
	"time"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/database"
)

// wrapServiceErr 把仓储层的原始错误统一转成业务错误。
//
// 已经是 *api.Error 的原样返回（保留精确的业务码），
// 其余一律归为内部错误 —— 绝不把数据库错误细节暴露给客户端。
func wrapServiceErr(err error) error {
	if err == nil {
		return nil
	}
	var ae *api.Error
	if errors.As(err, &ae) {
		return ae
	}
	if database.IsNotFound(err) {
		return api.NewError(api.CodeNotFound)
	}
	if database.IsDuplicate(err) {
		return api.WrapError(api.CodeConflict, err)
	}
	return api.WrapError(api.CodeInternal, err)
}

// timeLayouts 是前端可能提交的时间格式。
//
// 统一在这里解析并转为 UTC —— 业务层拿到的永远是 UTC，
// 不需要在各处判断"这个时间是本地的还是 UTC 的"。
var timeLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

// parseTimeInput 解析可选时间输入，空串返回 nil。
//
// 不带时区信息的格式按 UTC 解析：前端提交前应统一转成 ISO8601（带 Z 或偏移），
// 这是 web 端最不容易出错的约定。
func parseTimeInput(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			u := t.UTC()
			return &u, nil
		}
	}
	return nil, errors.New("无法解析时间: " + s)
}

// parseTimeInputInZone 按指定时区解析不带时区信息的时间（后台筛选用）。
func parseTimeInputInZone(s, tz string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	// 带时区信息的直接按标准解析
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		u := t.UTC()
		return &u, nil
	}
	loc := loadLoc(tz)
	for _, layout := range timeLayouts[1:] {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			u := t.UTC()
			return &u, nil
		}
	}
	return nil, errors.New("无法解析时间: " + s)
}

// ParseAdminTime 解析后台筛选用的时间输入，按商城时区理解不带时区的字符串。
//
// 后台管理员输入 "2026-08-26" 时想表达的是"商城时区的这一天"，
// 直接按 UTC 解析会让 UTC+8 的商家筛出错位 8 小时的数据。
func ParseAdminTime(s, tz string) (*time.Time, error) {
	return parseTimeInputInZone(s, tz)
}

func loadLoc(tz string) *time.Location {
	if tz == "" {
		tz = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}
