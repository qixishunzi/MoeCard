package utils

import (
	"log/slog"
	"sync"
	"time"
)

// 时间处理规则（对应架构文档 §43）：
//   - 数据库统一存 UTC
//   - 只在**展示边界**（后台列表、邮件模板）按商城配置时区转换
//   - 业务代码里禁止出现硬编码时区

const defaultTimezone = "Asia/Shanghai"

var (
	locCache   sync.Map // string -> *time.Location
	badZones   sync.Map // string -> bool，已经警告过的时区名
	utcNowFunc = func() time.Time { return time.Now().UTC() }
)

// NowUTC 返回当前 UTC 时间。测试可通过 SetNowFunc 注入固定时间。
func NowUTC() time.Time { return utcNowFunc() }

// SetNowFunc 仅供测试使用。
func SetNowFunc(f func() time.Time) {
	if f == nil {
		utcNowFunc = func() time.Time { return time.Now().UTC() }
		return
	}
	utcNowFunc = f
}

// LoadLocation 按名称加载时区，带缓存；失败时回退到 Asia/Shanghai，再失败回退 UTC。
//
// 时区库由 cmd/server 通过 _ "time/tzdata" 编进二进制，不依赖操作系统。
//
// 回退是必要的兜底（不能因为一个配置项就 panic），但它同时也很危险：
// 悄悄按 UTC 显示的话，中国的店家看到的每个时间都早了 8 小时，
// 而界面上不会有任何异样。所以每个加载不出来的名字都要吼一声。
func LoadLocation(name string) *time.Location {
	if name == "" {
		name = defaultTimezone
	}
	if v, ok := locCache.Load(name); ok {
		return v.(*time.Location)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		// 同一个名字只警告一次，避免每次格式化时间都刷屏
		if _, warned := badZones.LoadOrStore(name, true); !warned {
			slog.Warn("时区加载失败，时间显示将回退",
				"timezone", name, "fallback", fallbackName(name), "error", err)
		}
		if name != defaultTimezone {
			return LoadLocation(defaultTimezone)
		}
		loc = time.UTC
	}
	locCache.Store(name, loc)
	return loc
}

func fallbackName(name string) string {
	if name != defaultTimezone {
		return defaultTimezone
	}
	return "UTC"
}

// FormatInZone 把 UTC 时间格式化为指定时区的字符串。
func FormatInZone(t time.Time, tz, layout string) string {
	if t.IsZero() {
		return ""
	}
	if layout == "" {
		layout = "2006-01-02 15:04:05"
	}
	return t.In(LoadLocation(tz)).Format(layout)
}

// FormatPtrInZone 是 FormatInZone 的指针版本，nil 返回空串。
func FormatPtrInZone(t *time.Time, tz, layout string) string {
	if t == nil {
		return ""
	}
	return FormatInZone(*t, tz, layout)
}

// StartOfDayUTC 返回指定时区当天 00:00:00 对应的 UTC 时间。
// Dashboard 的"今日订单"必须按商城时区切分，否则 UTC+8 的凌晨 8 点前会统计错日期。
func StartOfDayUTC(t time.Time, tz string) time.Time {
	loc := LoadLocation(tz)
	local := t.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).UTC()
}

// DayRangeUTC 返回指定时区某天的 [start, end) UTC 区间。
func DayRangeUTC(t time.Time, tz string) (time.Time, time.Time) {
	start := StartOfDayUTC(t, tz)
	return start, start.AddDate(0, 0, 1)
}

// Ptr 返回时间指针，简化可选时间字段赋值。
func Ptr(t time.Time) *time.Time { return &t }

// PtrOrNil 零值时间返回 nil。
func PtrOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
