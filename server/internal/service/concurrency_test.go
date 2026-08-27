package service

import (
	"strings"
	"sync"
	"testing"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/model"
)

// TestConcurrentOrders_NoDuplicateCodes 是本项目最重要的一个测试。
//
// 场景：100 个并发下单，商品只有 30 张卡密。
// 断言：
//  1. 恰好 30 单成功，70 单因库存不足被拒
//  2. 没有任何一张卡密被分配给两个订单
//  3. 卡密状态账目平衡：locked + unused == 总数
func TestConcurrentOrders_NoDuplicateCodes(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()
	product := env.seedProduct(cat.ID, model.DeliveryAuto, 1000, 0)

	const (
		totalCodes = 30
		concurrent = 100
	)
	env.seedCodes(product.ID, totalCodes)

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		succeed  []string
		outOfStk int
		other    []error
	)

	start := make(chan struct{})
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start // 让所有 goroutine 尽量同时冲进来

			res, err := env.createOrder(product.ID, 1, "buyer@example.com", "")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				succeed = append(succeed, res.Order.OrderNo)
			case api.AsError(err).Code == api.CodeProductOutOfStk:
				outOfStk++
			default:
				other = append(other, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if len(other) > 0 {
		t.Fatalf("出现非预期错误 %d 个，第一个: %v", len(other), other[0])
	}
	if len(succeed) != totalCodes {
		t.Errorf("期望 %d 单成功，实际 %d 单", totalCodes, len(succeed))
	}
	if outOfStk != concurrent-totalCodes {
		t.Errorf("期望 %d 单因库存不足失败，实际 %d 单", concurrent-totalCodes, outOfStk)
	}

	// 账目校验：不允许出现"卡密凭空消失或凭空多出"
	stats := env.codeStats(product.ID)
	if stats[model.CodeStatusLocked] != int64(totalCodes) {
		t.Errorf("期望锁定 %d 张卡密，实际 %d 张", totalCodes, stats[model.CodeStatusLocked])
	}
	if stats[model.CodeStatusUnused] != 0 {
		t.Errorf("期望剩余 0 张未使用卡密，实际 %d 张", stats[model.CodeStatusUnused])
	}

	// 核心断言：同一张卡密绝不能出现在两个订单里
	assertNoDuplicateCodeAssignment(t, env, product.ID)
}

// TestConcurrentOrders_MultiQuantity 验证一单多张时同样不会重复发卡。
func TestConcurrentOrders_MultiQuantity(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()
	product := env.seedProduct(cat.ID, model.DeliveryAuto, 1000, 0)

	const (
		totalCodes = 50
		perOrder   = 5
		concurrent = 30 // 30*5 = 150 > 50，必然有失败的
	)
	env.seedCodes(product.ID, totalCodes)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		succeed int
	)
	start := make(chan struct{})
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := env.createOrder(product.ID, perOrder, "buyer@example.com", ""); err == nil {
				mu.Lock()
				succeed++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if succeed != totalCodes/perOrder {
		t.Errorf("期望 %d 单成功，实际 %d 单", totalCodes/perOrder, succeed)
	}
	stats := env.codeStats(product.ID)
	if stats[model.CodeStatusLocked] != int64(succeed*perOrder) {
		t.Errorf("锁定数量对不上：期望 %d，实际 %d", succeed*perOrder, stats[model.CodeStatusLocked])
	}
	assertNoDuplicateCodeAssignment(t, env, product.ID)
}

// TestConcurrentOrders_ManualStock 验证手动发货商品不会超卖。
func TestConcurrentOrders_ManualStock(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()
	const stock = 20
	product := env.seedProduct(cat.ID, model.DeliveryManual, 1000, stock)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		succeed int
	)
	start := make(chan struct{})
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := env.createOrder(product.ID, 1, "buyer@example.com", ""); err == nil {
				mu.Lock()
				succeed++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if succeed != stock {
		t.Errorf("期望 %d 单成功（库存上限），实际 %d 单 —— 发生超卖！", stock, succeed)
	}
	p, err := env.repos.Product.FindByID(env.ctx, nil, product.ID)
	if err != nil {
		t.Fatalf("读取商品失败: %v", err)
	}
	if p.Stock != 0 {
		t.Errorf("期望库存扣至 0，实际 %d", p.Stock)
	}
}

// TestUnlimitedStock 验证 stock = -1 表示无限库存。
func TestUnlimitedStock(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()
	product := env.seedProduct(cat.ID, model.DeliveryManual, 1000, model.StockUnlimited)

	for i := 0; i < 50; i++ {
		if _, err := env.createOrder(product.ID, 1, "buyer@example.com", ""); err != nil {
			t.Fatalf("第 %d 单失败: %v", i+1, err)
		}
	}
	p, _ := env.repos.Product.FindByID(env.ctx, nil, product.ID)
	if p.Stock != model.StockUnlimited {
		t.Errorf("无限库存商品的 stock 不应被扣减，当前 %d", p.Stock)
	}
}

// assertNoDuplicateCodeAssignment 断言没有任何卡密被分配给多个订单，
// 也没有任何订单拿到重复的卡密内容。
func assertNoDuplicateCodeAssignment(t *testing.T, env *testEnv, productID uint64) {
	t.Helper()

	var codes []model.ProductCode
	if err := env.db.DB.WithContext(env.ctx).
		Where("product_id = ? AND status <> ?", productID, model.CodeStatusUnused).
		Find(&codes).Error; err != nil {
		t.Fatalf("读取卡密失败: %v", err)
	}

	seenContent := map[string]uint64{}
	for _, c := range codes {
		if c.OrderID == 0 {
			t.Errorf("卡密 #%d 状态为 %s 但没有关联订单", c.ID, c.Status)
			continue
		}
		if prev, dup := seenContent[c.Content]; dup {
			t.Errorf("卡密内容 %q 被重复分配：订单 #%d 与订单 #%d", c.Content, prev, c.OrderID)
			continue
		}
		seenContent[c.Content] = c.OrderID
	}

	// 反向校验：每个订单实际拿到的卡密数量必须等于购买数量
	type row struct {
		OrderID  uint64
		ItemID   uint64
		Quantity int
		Got      int
	}
	var rows []row
	if err := env.db.DB.WithContext(env.ctx).Raw(`
		SELECT oi.order_id AS order_id, oi.id AS item_id, oi.quantity AS quantity,
		       (SELECT COUNT(*) FROM product_codes pc WHERE pc.order_item_id = oi.id) AS got
		FROM order_items oi WHERE oi.product_id = ?`, productID).Scan(&rows).Error; err != nil {
		t.Fatalf("统计订单卡密失败: %v", err)
	}
	for _, r := range rows {
		if r.Got != r.Quantity {
			t.Errorf("订单 #%d 明细 #%d：购买 %d 张，实际分配 %d 张",
				r.OrderID, r.ItemID, r.Quantity, r.Got)
		}
	}
}

// TestConcurrentPaymentNotify_Idempotent 验证重复支付回调只发一次货。
//
// 场景：同一订单被并发推送 20 次支付成功通知。
// 断言：只有 1 次真正执行业务，其余全部识别为重复；
//
//	卡密只发一次，优惠券只核销一次，销量只加一次。
func TestConcurrentPaymentNotify_Idempotent(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()
	product := env.seedProduct(cat.ID, model.DeliveryAuto, 5000, 0)
	env.seedCodes(product.ID, 10)

	coupon := mustCreateCoupon(t, env, &CouponInput{
		Code: "IDEMP10", Type: model.CouponFixed, Value: 1000,
		Scope: model.CouponScopeAll, UsageLimit: 100, Status: model.StatusActive,
	})

	order := env.mustCreateOrder(product.ID, 2, "buyer@example.com", coupon.Code)
	want := int64(5000*2 - 1000)
	if order.Order.PayAmount != want {
		t.Fatalf("应付金额计算错误：期望 %d，实际 %d", want, order.Order.PayAmount)
	}

	const notifyTimes = 20
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		processed int
		duplicate int
		failures  []error
	)
	start := make(chan struct{})
	for i := 0; i < notifyTimes; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, err := env.pay(order.Order.OrderNo, "TRADE-123456", order.Order.PayAmount)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, err)
				return
			}
			if res.Duplicate {
				duplicate++
			} else {
				processed++
			}
		}()
	}
	close(start)
	wg.Wait()

	if len(failures) > 0 {
		t.Fatalf("支付回调出现错误 %d 次，第一个: %v", len(failures), failures[0])
	}
	if processed != 1 {
		t.Errorf("期望只有 1 次真正处理，实际 %d 次 —— 幂等失效！", processed)
	}
	if duplicate != notifyTimes-1 {
		t.Errorf("期望 %d 次被识别为重复，实际 %d 次", notifyTimes-1, duplicate)
	}

	// 卡密只能卖出 2 张
	stats := env.codeStats(product.ID)
	if stats[model.CodeStatusSold] != 2 {
		t.Errorf("期望售出 2 张卡密，实际 %d 张", stats[model.CodeStatusSold])
	}
	if stats[model.CodeStatusUnused] != 8 {
		t.Errorf("期望剩余 8 张未使用卡密，实际 %d 张", stats[model.CodeStatusUnused])
	}

	// 优惠券只核销一次
	c, err := env.repos.Coupon.FindByID(env.ctx, nil, coupon.ID)
	if err != nil {
		t.Fatalf("读取优惠券失败: %v", err)
	}
	if c.UsedCount != 1 {
		t.Errorf("期望优惠券核销 1 次，实际 %d 次 —— 重复核销！", c.UsedCount)
	}
	usages, total, err := env.repos.Coupon.ListUsages(env.ctx, nil, coupon.ID, 0, 100)
	if err != nil {
		t.Fatalf("读取核销记录失败: %v", err)
	}
	if total != 1 || len(usages) != 1 {
		t.Errorf("期望 1 条核销记录，实际 %d 条", total)
	}

	// 销量只加一次
	p, _ := env.repos.Product.FindByID(env.ctx, nil, product.ID)
	if p.SalesCount != 2 {
		t.Errorf("期望销量 +2，实际 %d", p.SalesCount)
	}

	// 订单状态与发货内容
	final, items, err := env.svc.Order.GetByNo(env.ctx, order.Order.OrderNo)
	if err != nil {
		t.Fatalf("读取订单失败: %v", err)
	}
	if final.Status != model.OrderCompleted {
		t.Errorf("期望订单状态 completed，实际 %s", final.Status)
	}
	if final.PaymentTradeNo != "TRADE-123456" {
		t.Errorf("支付流水号未正确记录: %q", final.PaymentTradeNo)
	}
	if len(items) != 1 {
		t.Fatalf("期望 1 条订单明细，实际 %d 条", len(items))
	}
	lines := strings.Split(strings.TrimSpace(items[0].DeliveryContent), "\n")
	if len(lines) != 2 {
		t.Errorf("期望发货 2 行卡密，实际 %d 行: %q", len(lines), items[0].DeliveryContent)
	}
	if lines[0] == lines[1] {
		t.Errorf("同一订单内发出了两张相同的卡密: %q", lines[0])
	}
}

// TestPaymentAmountMismatch 验证金额被篡改的回调会被拒绝。
func TestPaymentAmountMismatch(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()
	product := env.seedProduct(cat.ID, model.DeliveryAuto, 10000, 0)
	env.seedCodes(product.ID, 5)

	order := env.mustCreateOrder(product.ID, 1, "buyer@example.com", "")

	// 攻击者把金额改成 1 分
	_, err := env.pay(order.Order.OrderNo, "FAKE-TRADE", 1)
	if err == nil {
		t.Fatal("金额被篡改的回调竟然通过了！")
	}
	if code := api.AsError(err).Code; code != api.CodePaymentAmountMismatch {
		t.Errorf("期望错误码 %d，实际 %d", api.CodePaymentAmountMismatch, code)
	}

	// 订单必须保持未支付，卡密不能被发出
	o, _, _ := env.svc.Order.GetByNo(env.ctx, order.Order.OrderNo)
	if o.IsPaidLike() {
		t.Error("金额校验失败后订单不应变为已支付")
	}
	if env.codeStats(product.ID)[model.CodeStatusSold] != 0 {
		t.Error("金额校验失败后不应发出任何卡密")
	}
}

func mustCreateCoupon(t *testing.T, env *testEnv, in *CouponInput) *model.Coupon {
	t.Helper()
	c, err := env.svc.Coupon.Create(env.ctx, in)
	if err != nil {
		t.Fatalf("创建优惠券失败: %v", err)
	}
	return c
}
