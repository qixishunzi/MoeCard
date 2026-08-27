package utils

import (
	"testing"
	"time"
	// 与 cmd/server 保持一致：不嵌时区库的话，这些用例在 Windows
	// 或精简 Linux 镜像上会因为环境「恰好有 tzdata」而假通过。
	_ "time/tzdata"
)

// TestNamedTimezonesLoad 是一条部署级回归。
//
// 踩过的坑：Windows 没有系统时区库，Go 会退回去读 $GOROOT/lib/time/zoneinfo.zip；
// 而发布用的 -trimpath 把 runtime.GOROOT() 清成了空字符串，那条退路也断了。
// 于是开发机上一切正常，打包出去的程序里 "Asia/Shanghai" 却加载不出来 ——
// 后台存不进时区，所有时间还静默按 UTC 显示，整整差 8 小时。
//
// 这条用例只在「时区库真的可用」时才通过，因此能挡住那次回归。
func TestNamedTimezonesLoad(t *testing.T) {
	for _, name := range []string{
		"Asia/Shanghai", "Asia/Tokyo", "Asia/Hong_Kong", "Asia/Taipei",
		"America/New_York", "Europe/London", "UTC",
	} {
		if _, err := time.LoadLocation(name); err != nil {
			t.Errorf("加载时区 %s 失败: %v（发布二进制需要 _ \"time/tzdata\"）", name, err)
		}
	}
}

// TestLoadLocationAppliesOffset 确认拿到的是真时区而不是悄悄退回的 UTC。
//
// 只断言"没报错"是不够的：回退逻辑本身就不报错，
// 必须验证换算出来的时间确实带了偏移。
func TestLoadLocationAppliesOffset(t *testing.T) {
	utc := time.Date(2026, 8, 26, 17, 3, 31, 0, time.UTC)

	sh := LoadLocation("Asia/Shanghai")
	if got := utc.In(sh).Format("2006-01-02 15:04"); got != "2026-08-27 01:03" {
		t.Errorf("Asia/Shanghai 换算错误: got %s, want 2026-08-27 01:03", got)
	}
	if _, off := utc.In(sh).Zone(); off != 8*3600 {
		t.Errorf("Asia/Shanghai 偏移应为 +8h，实际 %ds —— 多半是悄悄回退到 UTC 了", off)
	}

	ny := LoadLocation("America/New_York")
	if _, off := utc.In(ny).Zone(); off != -4*3600 {
		t.Errorf("America/New_York 夏令时偏移应为 -4h，实际 %ds", off)
	}
}

// TestLoadLocationFallback 名字非法时回退到默认时区，且不 panic。
func TestLoadLocationFallback(t *testing.T) {
	loc := LoadLocation("Mars/Olympus_Mons")
	if loc == nil {
		t.Fatal("回退结果不能为 nil")
	}
	if loc.String() != "Asia/Shanghai" {
		t.Errorf("非法时区应回退到 Asia/Shanghai，实际 %s", loc)
	}
}

// TestFormatInZone 覆盖展示边界的格式化。
func TestFormatInZone(t *testing.T) {
	utc := time.Date(2026, 8, 26, 17, 3, 31, 0, time.UTC)
	if got := FormatInZone(utc, "Asia/Shanghai", ""); got != "2026-08-27 01:03:31" {
		t.Errorf("默认格式错误: %s", got)
	}
	if got := FormatInZone(utc, "UTC", "2006-01-02"); got != "2026-08-26" {
		t.Errorf("自定义格式错误: %s", got)
	}
	if got := FormatInZone(time.Time{}, "Asia/Shanghai", ""); got != "" {
		t.Errorf("零值时间应返回空串，实际 %q", got)
	}
}
