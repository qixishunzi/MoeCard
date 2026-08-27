package repository

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/moecard/server/internal/model"
)

// TestProductUpdatePersistsEveryEditableColumn 是一条"白名单漏列"的兜底。
//
// ProductRepo.Update 用 Select 白名单挑要写的列。漏掉一列不会报错：
// 接口照样 200、前端照样弹"保存成功"，只有那一列永远写不进去。
// custom_fields（下单时买家要填的信息）和 low_stock_threshold 就这样漏过一次。
//
// 这里逐列改值再读回来对比，任何一列漏进白名单都会在这里炸出来。
func TestProductUpdatePersistsEveryEditableColumn(t *testing.T) {
	db := openTestDB(t)
	repo := &ProductRepo{}
	ctx := context.Background()

	p := &model.Product{
		CategoryID: 1, Name: "旧名", Slug: "old-slug", Price: 1000,
		DeliveryType: model.DeliveryManual, Status: model.ProductStatusOff,
		Stock: 5, MinQuantity: 1, MaxQuantity: 10, SalesCount: 42,
	}
	if err := repo.Create(ctx, db, p); err != nil {
		t.Fatalf("create: %v", err)
	}

	notified := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := db.Model(&model.Product{}).Where("id = ?", p.ID).
		Update("low_stock_notified_at", notified).Error; err != nil {
		t.Fatalf("seed notified: %v", err)
	}

	edited := *p
	edited.CategoryID = 2
	edited.Name = "新名"
	edited.Slug = "new-slug"
	edited.Cover = "/img/a.png"
	edited.Summary = "摘要"
	edited.Description = "<p>详情</p>"
	edited.Price = 2500
	edited.OriginalPrice = 3000
	edited.Stock = 7
	edited.DeliveryType = model.DeliveryManual
	edited.Status = model.ProductStatusOn
	edited.Sort = 9
	edited.IsRecommend = true
	edited.MinQuantity = 2
	edited.MaxQuantity = 20
	edited.LowStockThreshold = 3
	edited.LowStockNotifiedAt = nil // 补货后清空告警标记，必须真的落库
	edited.CustomFields = `[{"key":"uid","label":"游戏 UID","type":"text","required":true}]`

	if err := repo.Update(ctx, db, &edited); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := repo.FindByID(ctx, db, p.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	checks := []struct {
		col        string
		want, have any
	}{
		{"category_id", edited.CategoryID, got.CategoryID},
		{"name", edited.Name, got.Name},
		{"slug", edited.Slug, got.Slug},
		{"cover", edited.Cover, got.Cover},
		{"summary", edited.Summary, got.Summary},
		{"description", edited.Description, got.Description},
		{"price", edited.Price, got.Price},
		{"original_price", edited.OriginalPrice, got.OriginalPrice},
		{"stock", edited.Stock, got.Stock},
		{"delivery_type", edited.DeliveryType, got.DeliveryType},
		{"status", edited.Status, got.Status},
		{"sort", edited.Sort, got.Sort},
		{"is_recommend", edited.IsRecommend, got.IsRecommend},
		{"min_quantity", edited.MinQuantity, got.MinQuantity},
		{"max_quantity", edited.MaxQuantity, got.MaxQuantity},
		{"low_stock_threshold", edited.LowStockThreshold, got.LowStockThreshold},
		{"custom_fields", edited.CustomFields, got.CustomFields},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.want, c.have) {
			t.Errorf("%s 没写进数据库: want %v, got %v", c.col, c.want, c.have)
		}
	}
	if got.LowStockNotifiedAt != nil {
		t.Errorf("low_stock_notified_at 应被清空，仍是 %v", got.LowStockNotifiedAt)
	}

	// 反过来：不该被这条路径改的列必须原封不动
	if got.SalesCount != 42 {
		t.Errorf("sales_count 被覆盖了: %d", got.SalesCount)
	}
	if got.DeletedAt != nil {
		t.Errorf("deleted_at 被写了: %v", got.DeletedAt)
	}
}
