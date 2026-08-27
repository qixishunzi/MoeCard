package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/moecard/server/internal/config"
	"github.com/moecard/server/internal/database"
	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/utils"
)

// testEnv 是一套完整的测试环境：真实 SQLite 库 + 全部服务。
//
// 不使用 mock：并发安全与事务语义只有跑真实数据库才测得出来。
// mock 掉数据库的"并发测试"什么都证明不了。
type testEnv struct {
	t      *testing.T
	db     *database.DB
	repos  *repository.Repositories
	svc    *Services
	ctx    context.Context
	cancel context.CancelFunc
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	_ = logger.Init(logger.Config{Level: "error", Format: "text"})

	dir := t.TempDir()
	cfg := config.Default()
	cfg.App.Env = config.EnvDevelopment
	cfg.App.JWTSecret = "test-secret-for-unit-tests-only-000000"
	cfg.App.JWTExpire = time.Hour
	cfg.App.BaseURL = "http://127.0.0.1:8080"
	cfg.App.FrontendURL = "http://127.0.0.1:5173"
	cfg.Database.Driver = config.DriverSQLite
	cfg.Database.SQLite.Path = filepath.Join(dir, "test.db")
	cfg.Storage.LocalPath = filepath.Join(dir, "uploads")

	db, err := database.Open(cfg)
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("测试数据库迁移失败: %v", err)
	}

	repos := repository.New(db)
	svc, err := New(cfg, db, repos)
	if err != nil {
		t.Fatalf("构造服务失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	env := &testEnv{t: t, db: db, repos: repos, svc: svc, ctx: ctx, cancel: cancel}

	t.Cleanup(func() {
		cancel()
		_ = db.Close()
	})
	return env
}

// seedCategory 创建一个测试分类。
func (e *testEnv) seedCategory() *model.Category {
	e.t.Helper()
	c, err := e.svc.Category.Create(e.ctx, &CategoryInput{
		Name: "测试分类", Slug: "test-cat-" + utils.RandomHex(4), Status: model.StatusActive,
	})
	if err != nil {
		e.t.Fatalf("创建分类失败: %v", err)
	}
	return c
}

// seedProduct 创建一个测试商品。
func (e *testEnv) seedProduct(categoryID uint64, deliveryType string, price int64, stock int64) *model.Product {
	e.t.Helper()
	p, err := e.svc.Product.Create(e.ctx, &ProductInput{
		CategoryID:   categoryID,
		Name:         "测试商品",
		Slug:         "test-product-" + utils.RandomHex(4),
		Price:        price,
		Stock:        &stock,
		DeliveryType: deliveryType,
		Status:       model.ProductStatusOn,
		MinQuantity:  1,
		MaxQuantity:  100,
	})
	if err != nil {
		e.t.Fatalf("创建商品失败: %v", err)
	}
	return p
}

// seedCodes 为商品导入 n 条卡密。
func (e *testEnv) seedCodes(productID uint64, n int) {
	e.t.Helper()
	content := ""
	for i := 0; i < n; i++ {
		content += formatCode(i) + "\n"
	}
	res, err := e.svc.Code.Import(e.ctx, productID, content)
	if err != nil {
		e.t.Fatalf("导入卡密失败: %v", err)
	}
	if res.Imported != n {
		e.t.Fatalf("期望导入 %d 条卡密，实际 %d 条", n, res.Imported)
	}
}

func formatCode(i int) string {
	const digits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	buf := []byte("CODE-0000-0000")
	buf[5] = digits[(i/36/36/36)%36]
	buf[6] = digits[(i/36/36)%36]
	buf[7] = digits[(i/36)%36]
	buf[8] = digits[i%36]
	buf[10] = digits[(i/7)%36]
	buf[11] = digits[(i/13)%36]
	buf[12] = digits[(i/17)%36]
	buf[13] = digits[(i/23)%36]
	return string(buf)
}

// createOrder 创建一个订单。
func (e *testEnv) createOrder(productID uint64, qty int, email, coupon string) (*CreateOrderResult, error) {
	return e.svc.Order.CreateOrder(e.ctx, CreateOrderInput{
		ProductID:  productID,
		Quantity:   qty,
		Email:      email,
		CouponCode: coupon,
		ClientIP:   "127.0.0.1",
	})
}

// mustCreateOrder 创建订单，失败即终止测试。
func (e *testEnv) mustCreateOrder(productID uint64, qty int, email, coupon string) *CreateOrderResult {
	e.t.Helper()
	res, err := e.createOrder(productID, qty, email, coupon)
	if err != nil {
		e.t.Fatalf("创建订单失败: %v", err)
	}
	return res
}

// pay 模拟一次支付成功回调。
func (e *testEnv) pay(orderNo, tradeNo string, amount int64) (*PaymentSuccessResult, error) {
	return e.svc.Order.HandlePaymentSuccess(e.ctx, PaymentSuccessInput{
		OrderNo:   orderNo,
		TradeNo:   tradeNo,
		Amount:    amount,
		Provider:  "test",
		ChannelID: 0,
		ClientIP:  "127.0.0.1",
	})
}

// availableCodes 返回商品当前可用卡密数。
func (e *testEnv) availableCodes(productID uint64) int64 {
	e.t.Helper()
	n, err := e.repos.Code.CountAvailable(e.ctx, nil, productID)
	if err != nil {
		e.t.Fatalf("统计可用卡密失败: %v", err)
	}
	return n
}

// codeStats 返回商品各状态卡密数量。
func (e *testEnv) codeStats(productID uint64) map[string]int64 {
	e.t.Helper()
	s, err := e.repos.Code.StatsByProduct(e.ctx, nil, productID)
	if err != nil {
		e.t.Fatalf("统计卡密失败: %v", err)
	}
	return s
}
