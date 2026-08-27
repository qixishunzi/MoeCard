package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/moecard/server/internal/model"
)

// openTestDB 建一个临时 SQLite 库，只造这几个测试要用的表。
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "t.db")),
		&gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Windows 上不关连接就删不掉临时文件，t.TempDir 的清理会报错
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.Order{}, &model.ProductCode{}, &model.Product{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// TestFirstSeenAtReadsTimestamp 覆盖一个真实踩过的坑：
// 用 SELECT MIN(updated_at) 扫进匿名结构体时，SQLite 驱动返回的是字符串，
// time.Time 字段会被静默留空 —— 不报错，只是永远拿到零值，
// 于是"趋势从部署那天开始"这个功能完全失效却没有任何迹象。
func TestFirstSeenAtReadsTimestamp(t *testing.T) {
	db := openTestDB(t)
	repo := &SettingRepo{}

	oldest := time.Date(2026, 3, 5, 8, 30, 0, 0, time.UTC)
	rows := []model.SystemSetting{
		{Key: "b", Value: "2", UpdatedAt: oldest.Add(48 * time.Hour)},
		{Key: "a", Value: "1", UpdatedAt: oldest},
		{Key: "c", Value: "3", UpdatedAt: oldest.Add(72 * time.Hour)},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := repo.FirstSeenAt(context.Background(), db)
	if err != nil {
		t.Fatalf("FirstSeenAt: %v", err)
	}
	if got.IsZero() {
		t.Fatal("拿到零值：时间戳没被解析出来")
	}
	if !got.UTC().Equal(oldest) {
		t.Fatalf("want %s, got %s", oldest, got.UTC())
	}
}

// TestFirstSeenAtEmpty 空表返回零值且不报错 —— 调用方按"部署时间未知"处理。
func TestFirstSeenAtEmpty(t *testing.T) {
	repo := &SettingRepo{}
	got, err := repo.FirstSeenAt(context.Background(), openTestDB(t))
	if err != nil {
		t.Fatalf("空表不该报错: %v", err)
	}
	if !got.IsZero() {
		t.Fatalf("空表应返回零值，得到 %s", got)
	}
}

// TestRevenueByDayParsesPaidAt 同一类扫描风险的回归：
// RevenueByDay 把 paid_at 扫进匿名结构体，一旦解析不出时间，
// 所有订单都会落到同一个零值日期上，图表看起来"有数据"但全错。
func TestRevenueByDayParsesPaidAt(t *testing.T) {
	db := openTestDB(t)
	repo := &OrderRepo{}

	day := time.Date(2026, 5, 10, 3, 0, 0, 0, time.UTC)
	paid1, paid2 := day, day.Add(26*time.Hour)
	orders := []model.Order{
		{OrderNo: "A1", QueryToken: "t1", Status: model.OrderCompleted, PayAmount: 1500, PaidAt: &paid1},
		{OrderNo: "A2", QueryToken: "t2", Status: model.OrderCompleted, PayAmount: 2500, PaidAt: &paid1},
		{OrderNo: "A3", QueryToken: "t3", Status: model.OrderCompleted, PayAmount: 700, PaidAt: &paid2},
	}
	if err := db.Create(&orders).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	start := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 3)
	points, err := repo.RevenueByDay(context.Background(), db, start, end, "UTC")
	if err != nil {
		t.Fatalf("RevenueByDay: %v", err)
	}
	if len(points) != 3 {
		t.Fatalf("want 3 天, got %d", len(points))
	}
	if points[0].Date != "2026-05-10" || points[0].Orders != 2 || points[0].Revenue != 4000 {
		t.Fatalf("第 1 天错: %+v", points[0])
	}
	if points[1].Date != "2026-05-11" || points[1].Orders != 1 || points[1].Revenue != 700 {
		t.Fatalf("第 2 天错: %+v", points[1])
	}
	if points[2].Orders != 0 || points[2].Revenue != 0 {
		t.Fatalf("第 3 天应为空: %+v", points[2])
	}
}
