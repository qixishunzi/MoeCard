package service

import (
	"testing"
	"time"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/utils"
)

// TestOrderStatusMachine 验证状态机拒绝所有非法转换。
func TestOrderStatusMachine(t *testing.T) {
	legal := []struct{ from, to string }{
		{model.OrderPending, model.OrderPaying},
		{model.OrderPending, model.OrderPaid},
		{model.OrderPending, model.OrderCancelled},
		{model.OrderPending, model.OrderExpired},
		{model.OrderPaying, model.OrderPaid},
		{model.OrderPaying, model.OrderPending},
		{model.OrderPaid, model.OrderWaitingDelivery},
		{model.OrderPaid, model.OrderCompleted},
		{model.OrderPaid, model.OrderRefunded},
		{model.OrderWaitingDelivery, model.OrderCompleted},
		{model.OrderWaitingDelivery, model.OrderRefunded},
		{model.OrderCompleted, model.OrderRefunded},
	}
	for _, tc := range legal {
		if !model.CanTransition(tc.from, tc.to) {
			t.Errorf("%s → %s 应当是合法转换", tc.from, tc.to)
		}
	}

	// 这些非法转换如果被放行，就意味着"已完成订单能被改回待付款"
	// 之类的灾难性数据错乱。
	illegal := []struct{ from, to string }{
		{model.OrderCompleted, model.OrderPending},
		{model.OrderCompleted, model.OrderPaid},
		{model.OrderCompleted, model.OrderCancelled},
		{model.OrderExpired, model.OrderPaid},
		{model.OrderExpired, model.OrderPending},
		{model.OrderCancelled, model.OrderPaid},
		{model.OrderRefunded, model.OrderCompleted},
		{model.OrderRefunded, model.OrderPaid},
		{model.OrderPending, model.OrderCompleted},
		{model.OrderPending, model.OrderWaitingDelivery},
		{model.OrderPaid, model.OrderPending},
		{model.OrderPaid, model.OrderPaying},
		{model.OrderPending, model.OrderPending},
	}
	for _, tc := range illegal {
		if model.CanTransition(tc.from, tc.to) {
			t.Errorf("%s → %s 必须被拒绝", tc.from, tc.to)
		}
	}

	// TransitionTo 必须返回错误而不是静默改状态
	o := &model.Order{Status: model.OrderCompleted}
	if err := o.TransitionTo(model.OrderPending); err == nil {
		t.Error("TransitionTo 应当拒绝非法转换")
	}
	if o.Status != model.OrderCompleted {
		t.Errorf("非法转换后状态不应被修改，当前 %s", o.Status)
	}
	if err := o.TransitionTo("no-such-status"); err == nil {
		t.Error("TransitionTo 应当拒绝未知状态")
	}
}

// TestOrderExpiry_ReleasesStock 验证订单过期会释放预占的库存。
func TestOrderExpiry_ReleasesStock(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()

	auto := env.seedProduct(cat.ID, model.DeliveryAuto, 1000, 0)
	env.seedCodes(auto.ID, 5)
	manual := env.seedProduct(cat.ID, model.DeliveryManual, 1000, 5)

	orderAuto := env.mustCreateOrder(auto.ID, 2, "buyer@example.com", "")
	orderManual := env.mustCreateOrder(manual.ID, 2, "buyer@example.com", "")

	if got := env.availableCodes(auto.ID); got != 3 {
		t.Fatalf("下单后应剩 3 张可用卡密，实际 %d", got)
	}
	p, _ := env.repos.Product.FindByID(env.ctx, nil, manual.ID)
	if p.Stock != 3 {
		t.Fatalf("下单后手动商品库存应为 3，实际 %d", p.Stock)
	}

	// 把两个订单的过期时间往前推
	past := utils.NowUTC().Add(-time.Minute)
	for _, no := range []string{orderAuto.Order.OrderNo, orderManual.Order.OrderNo} {
		if err := env.db.DB.Exec("UPDATE orders SET expired_at = ? WHERE order_no = ?", past, no).Error; err != nil {
			t.Fatal(err)
		}
	}

	n, err := env.svc.Order.ExpireDueOrders(env.ctx, 100)
	if err != nil {
		t.Fatalf("处理过期订单失败: %v", err)
	}
	if n != 2 {
		t.Errorf("期望处理 2 个过期订单，实际 %d 个", n)
	}

	// 库存必须归还
	if got := env.availableCodes(auto.ID); got != 5 {
		t.Errorf("过期后卡密应全部释放（5 张），实际 %d 张", got)
	}
	p, _ = env.repos.Product.FindByID(env.ctx, nil, manual.ID)
	if p.Stock != 5 {
		t.Errorf("过期后手动商品库存应恢复为 5，实际 %d", p.Stock)
	}

	o, _, _ := env.svc.Order.GetByNo(env.ctx, orderAuto.Order.OrderNo)
	if o.Status != model.OrderExpired {
		t.Errorf("期望订单状态 expired，实际 %s", o.Status)
	}
}

// TestLatePaymentAfterExpiry 验证「支付回调晚于订单过期」的处理。
//
// 这是最容易造成资损的边界场景：钱已经收到，但订单已过期、库存已释放。
// 正确行为是复活订单并重新占用库存，绝不能吞掉这笔钱。
func TestLatePaymentAfterExpiry(t *testing.T) {
	t.Run("库存充足时应复活并正常发货", func(t *testing.T) {
		env := newTestEnv(t)
		cat := env.seedCategory()
		product := env.seedProduct(cat.ID, model.DeliveryAuto, 1000, 0)
		env.seedCodes(product.ID, 5)

		order := env.mustCreateOrder(product.ID, 1, "buyer@example.com", "")
		expireOrder(t, env, order.Order.OrderNo)

		if got := env.availableCodes(product.ID); got != 5 {
			t.Fatalf("过期后应释放库存，当前可用 %d 张", got)
		}

		// 迟到的支付回调
		res, err := env.pay(order.Order.OrderNo, "LATE-TRADE", order.Order.PayAmount)
		if err != nil {
			t.Fatalf("迟到的支付回调不应失败: %v", err)
		}
		if !res.Delivered {
			t.Error("库存充足时迟到回调应当完成发货")
		}
		if res.Attention {
			t.Errorf("库存充足时不应标记需人工处理: %s", res.Order.AttentionReason)
		}

		o, items, _ := env.svc.Order.GetByNo(env.ctx, order.Order.OrderNo)
		if o.Status != model.OrderCompleted {
			t.Errorf("期望订单状态 completed，实际 %s", o.Status)
		}
		if items[0].DeliveryContent == "" {
			t.Error("发货内容不应为空")
		}
		if env.codeStats(product.ID)[model.CodeStatusSold] != 1 {
			t.Error("应当售出 1 张卡密")
		}
	})

	t.Run("库存已售罄时应标记人工处理而不是丢单", func(t *testing.T) {
		env := newTestEnv(t)
		cat := env.seedCategory()
		product := env.seedProduct(cat.ID, model.DeliveryAuto, 1000, 0)
		env.seedCodes(product.ID, 1) // 只有 1 张

		order := env.mustCreateOrder(product.ID, 1, "buyer@example.com", "")
		expireOrder(t, env, order.Order.OrderNo)

		// 库存被别人买走
		other := env.mustCreateOrder(product.ID, 1, "other@example.com", "")
		if _, err := env.pay(other.Order.OrderNo, "T-OTHER", other.Order.PayAmount); err != nil {
			t.Fatalf("其他订单支付失败: %v", err)
		}
		if got := env.availableCodes(product.ID); got != 0 {
			t.Fatalf("此时应无可用卡密，实际 %d 张", got)
		}

		// 迟到回调进来：**绝不能报错丢单**
		res, err := env.pay(order.Order.OrderNo, "LATE-TRADE", order.Order.PayAmount)
		if err != nil {
			t.Fatalf("即使无货，已收到的钱也必须被记录，不能返回错误: %v", err)
		}
		if !res.Attention {
			t.Error("无货时必须标记 needs_attention 供人工介入")
		}
		if res.Delivered {
			t.Error("无货时不应标记为已发货")
		}

		o, _, _ := env.svc.Order.GetByNo(env.ctx, order.Order.OrderNo)
		if o.Status != model.OrderWaitingDelivery {
			t.Errorf("期望转入 waiting_delivery 等待人工处理，实际 %s", o.Status)
		}
		if o.PaidAt == nil {
			t.Error("必须记录支付时间 —— 钱确实收到了")
		}
		if o.AttentionReason == "" {
			t.Error("必须写明异常原因")
		}

		// 管理员补货后手动发货应当可行
		if _, _, err := env.svc.Order.ManualDeliver(env.ctx, o.ID, "补发的卡密 XXXX-YYYY"); err != nil {
			t.Fatalf("人工补发应当成功: %v", err)
		}
		o, _, _ = env.svc.Order.GetByNo(env.ctx, order.Order.OrderNo)
		if o.Status != model.OrderCompleted {
			t.Errorf("人工发货后应为 completed，实际 %s", o.Status)
		}
		if o.NeedsAttention {
			t.Error("人工发货后应清除异常标记")
		}
	})
}

// TestOrderQueryIDOR 验证订单查询的越权防护。
func TestOrderQueryIDOR(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()
	product := env.seedProduct(cat.ID, model.DeliveryManual, 1000, 100)

	order := env.mustCreateOrder(product.ID, 1, "victim@example.com", "")

	t.Run("错误邮箱返回订单不存在", func(t *testing.T) {
		_, _, err := env.svc.Order.QueryByNoAndEmail(env.ctx, order.Order.OrderNo, "attacker@evil.com")
		assertCode(t, err, api.CodeOrderNotFound)
	})

	t.Run("正确邮箱可查", func(t *testing.T) {
		o, _, err := env.svc.Order.QueryByNoAndEmail(env.ctx, order.Order.OrderNo, "victim@example.com")
		if err != nil {
			t.Fatalf("本人查询应当成功: %v", err)
		}
		if o.OrderNo != order.Order.OrderNo {
			t.Error("返回了错误的订单")
		}
	})

	t.Run("邮箱大小写不敏感", func(t *testing.T) {
		if _, _, err := env.svc.Order.QueryByNoAndEmail(env.ctx, order.Order.OrderNo, "Victim@Example.COM"); err != nil {
			t.Errorf("邮箱应当大小写不敏感: %v", err)
		}
	})

	t.Run("token 可免邮箱查询", func(t *testing.T) {
		if _, _, err := env.svc.Order.QueryByToken(env.ctx, order.Token); err != nil {
			t.Fatalf("token 查询应当成功: %v", err)
		}
	})

	t.Run("错误 token 被拒绝", func(t *testing.T) {
		_, _, err := env.svc.Order.QueryByToken(env.ctx, utils.RandomHex(16))
		assertCode(t, err, api.CodeOrderNotFound)
	})

	t.Run("token 长度不对直接拒绝", func(t *testing.T) {
		_, _, err := env.svc.Order.QueryByToken(env.ctx, "short")
		assertCode(t, err, api.CodeOrderNotFound)
	})
}

// TestOrderCancel 验证取消订单会释放库存。
func TestOrderCancel(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()
	product := env.seedProduct(cat.ID, model.DeliveryAuto, 1000, 0)
	env.seedCodes(product.ID, 3)

	order := env.mustCreateOrder(product.ID, 2, "buyer@example.com", "")
	if got := env.availableCodes(product.ID); got != 1 {
		t.Fatalf("下单后应剩 1 张，实际 %d", got)
	}

	t.Run("他人无法取消", func(t *testing.T) {
		err := env.svc.Order.Cancel(env.ctx, order.Order.OrderNo, "attacker@evil.com")
		assertCode(t, err, api.CodeOrderNotFound)
	})

	t.Run("本人可取消并释放库存", func(t *testing.T) {
		if err := env.svc.Order.Cancel(env.ctx, order.Order.OrderNo, "buyer@example.com"); err != nil {
			t.Fatalf("取消失败: %v", err)
		}
		if got := env.availableCodes(product.ID); got != 3 {
			t.Errorf("取消后应释放全部卡密（3 张），实际 %d 张", got)
		}
	})

	t.Run("已取消的订单不能再取消", func(t *testing.T) {
		err := env.svc.Order.Cancel(env.ctx, order.Order.OrderNo, "buyer@example.com")
		assertCode(t, err, api.CodeOrderStatusInvld)
	})

	t.Run("已支付订单不能取消", func(t *testing.T) {
		o2 := env.mustCreateOrder(product.ID, 1, "buyer2@example.com", "")
		if _, err := env.pay(o2.Order.OrderNo, "T2", o2.Order.PayAmount); err != nil {
			t.Fatal(err)
		}
		err := env.svc.Order.Cancel(env.ctx, o2.Order.OrderNo, "buyer2@example.com")
		assertCode(t, err, api.CodeOrderStatusInvld)
	})
}

// TestOrderSnapshot 验证订单保存商品快照，商品改名/删除不影响历史订单。
func TestOrderSnapshot(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()
	product := env.seedProduct(cat.ID, model.DeliveryManual, 5000, 100)

	order := env.mustCreateOrder(product.ID, 2, "buyer@example.com", "")
	originalName := order.Items[0].ProductName

	// 改价 + 改名 + 软删
	if _, err := env.svc.Product.Update(env.ctx, product.ID, &ProductInput{
		CategoryID: cat.ID, Name: "改名后的商品", Slug: product.Slug,
		Price: 99999, DeliveryType: model.DeliveryManual,
		Status: model.ProductStatusOff, MinQuantity: 1, MaxQuantity: 10,
	}); err != nil {
		t.Fatalf("更新商品失败: %v", err)
	}
	if err := env.svc.Product.Delete(env.ctx, product.ID); err != nil {
		t.Fatalf("删除商品失败: %v", err)
	}

	_, items, err := env.svc.Order.GetByNo(env.ctx, order.Order.OrderNo)
	if err != nil {
		t.Fatalf("商品被删除后订单应仍可查询: %v", err)
	}
	if items[0].ProductName != originalName {
		t.Errorf("订单应保留下单时的商品名快照，期望 %q，实际 %q", originalName, items[0].ProductName)
	}
	if items[0].ProductPrice != 5000 {
		t.Errorf("订单应保留下单时的价格快照，期望 5000，实际 %d", items[0].ProductPrice)
	}
	if items[0].Subtotal != 10000 {
		t.Errorf("小计应为 10000，实际 %d", items[0].Subtotal)
	}
}

// TestOrderNoUniqueness 验证订单号唯一且不可预测。
func TestOrderNoUniqueness(t *testing.T) {
	seen := make(map[string]bool, 5000)
	for i := 0; i < 5000; i++ {
		no := utils.GenerateOrderNo()
		if seen[no] {
			t.Fatalf("订单号发生碰撞: %s", no)
		}
		seen[no] = true
		if len(no) != 24 {
			t.Fatalf("订单号长度应为 24，实际 %d: %s", len(no), no)
		}
	}
}

// TestQueryTokenEntropy 验证查询 token 的随机性。
func TestQueryTokenEntropy(t *testing.T) {
	seen := make(map[string]bool, 5000)
	for i := 0; i < 5000; i++ {
		tk := utils.GenerateQueryToken()
		if len(tk) != 32 {
			t.Fatalf("token 长度应为 32，实际 %d", len(tk))
		}
		if seen[tk] {
			t.Fatalf("token 发生碰撞: %s", tk)
		}
		seen[tk] = true
	}
}

// TestRefund 验证退款流程。
func TestRefund(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()
	product := env.seedProduct(cat.ID, model.DeliveryManual, 10000, 100)

	c := mustCreateCoupon(t, env, &CouponInput{
		Code: "REFUND10", Type: model.CouponFixed, Value: 1000,
		Scope: model.CouponScopeAll, Status: model.StatusActive, UsageLimit: 10,
	})
	order := env.mustCreateOrder(product.ID, 1, "buyer@example.com", c.Code)
	if _, err := env.pay(order.Order.OrderNo, "T-REFUND", order.Order.PayAmount); err != nil {
		t.Fatal(err)
	}

	o, _, _ := env.svc.Order.GetByNo(env.ctx, order.Order.OrderNo)

	t.Run("退款金额超过实付被拒绝", func(t *testing.T) {
		_, err := env.svc.Order.MarkRefunded(env.ctx, o.ID, o.PayAmount+1, "test")
		assertCode(t, err, api.CodeValidation)
	})

	t.Run("全额退款归还优惠券额度", func(t *testing.T) {
		before, _ := env.repos.Coupon.FindByID(env.ctx, nil, c.ID)
		if before.UsedCount != 1 {
			t.Fatalf("支付后应核销 1 次，实际 %d", before.UsedCount)
		}
		refunded, err := env.svc.Order.MarkRefunded(env.ctx, o.ID, o.PayAmount, "用户申请退款")
		if err != nil {
			t.Fatalf("退款失败: %v", err)
		}
		if refunded.Status != model.OrderRefunded {
			t.Errorf("期望状态 refunded，实际 %s", refunded.Status)
		}
		if refunded.RefundAmount != o.PayAmount {
			t.Errorf("退款金额记录错误: %d", refunded.RefundAmount)
		}
		if refunded.RefundedAt == nil {
			t.Error("必须记录退款时间")
		}
		after, _ := env.repos.Coupon.FindByID(env.ctx, nil, c.ID)
		if after.UsedCount != 0 {
			t.Errorf("全额退款后应归还优惠券额度，当前 used_count = %d", after.UsedCount)
		}
	})

	t.Run("重复退款被拒绝", func(t *testing.T) {
		_, err := env.svc.Order.MarkRefunded(env.ctx, o.ID, 100, "again")
		assertCode(t, err, api.CodeOrderStatusInvld)
	})
}

// TestShopClosed 验证关闭下单开关后拒绝新订单。
func TestShopClosed(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()
	product := env.seedProduct(cat.ID, model.DeliveryManual, 1000, 100)

	if err := env.svc.Setting.Set(env.ctx, model.SetAllowOrder, "0"); err != nil {
		t.Fatal(err)
	}
	_, err := env.createOrder(product.ID, 1, "buyer@example.com", "")
	assertCode(t, err, api.CodeShopClosed)

	if err := env.svc.Setting.Set(env.ctx, model.SetAllowOrder, "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.createOrder(product.ID, 1, "buyer@example.com", ""); err != nil {
		t.Errorf("重新开放下单后应当成功: %v", err)
	}
}

// manualStock 供手动发货商品的测试用例取地址（ProductInput.Stock 是指针）。
var manualStock int64 = 1000

// TestQuantityLimits 验证购买数量范围校验。
func TestQuantityLimits(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()

	p, err := env.svc.Product.Create(env.ctx, &ProductInput{
		CategoryID: cat.ID, Name: "限购商品", Slug: "limited",
		Price: 1000, Stock: &manualStock, DeliveryType: model.DeliveryManual,
		Status: model.ProductStatusOn, MinQuantity: 2, MaxQuantity: 5,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, qty := range []int{1, 6, 100} {
		if _, err := env.createOrder(p.ID, qty, "buyer@example.com", ""); err == nil {
			t.Errorf("数量 %d 超出 [2,5] 范围，应当被拒绝", qty)
		} else {
			assertCode(t, err, api.CodeOrderQtyInvalid)
		}
	}
	for _, qty := range []int{2, 3, 5} {
		if _, err := env.createOrder(p.ID, qty, "buyer@example.com", ""); err != nil {
			t.Errorf("数量 %d 在允许范围内，却被拒绝: %v", qty, err)
		}
	}
}

func expireOrder(t *testing.T, env *testEnv, orderNo string) {
	t.Helper()
	past := utils.NowUTC().Add(-time.Minute)
	if err := env.db.DB.Exec("UPDATE orders SET expired_at = ? WHERE order_no = ?", past, orderNo).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.Order.ExpireDueOrders(env.ctx, 100); err != nil {
		t.Fatalf("处理过期订单失败: %v", err)
	}
}
