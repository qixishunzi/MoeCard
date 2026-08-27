package service

import (
	"testing"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/utils"
	"gorm.io/gorm"
)

// TestNotifyChannelMustMatchOrder 验证支付回调必须来自订单实际发起支付的渠道。
//
// 背景：商家同时配置多个渠道时（例如支付宝官方 + 某个易支付站点），
// 如果不校验渠道归属，弱签名渠道的密钥一旦泄露，
// 就能用它伪造回调结算任何一笔订单 —— 包括本该走强签名渠道的订单。
func TestNotifyChannelMustMatchOrder(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()
	p := env.seedProduct(cat.ID, model.DeliveryManual, 10000, 100)

	res := env.mustCreateOrder(p.ID, 1, "buyer@example.com", "")
	order := res.Order

	// 订单在渠道 7 上发起支付
	if err := env.db.Tx(env.ctx, func(tx *gorm.DB) error {
		return env.repos.Order.UpdateFields(env.ctx, tx, order.ID,
			map[string]any{"payment_channel_id": 7})
	}); err != nil {
		t.Fatalf("设置订单渠道失败: %v", err)
	}

	// 渠道 9 的回调不得结算这笔订单
	_, err := env.svc.Order.HandlePaymentSuccess(env.ctx, PaymentSuccessInput{
		OrderNo: order.OrderNo, TradeNo: "T-EVIL", Amount: order.PayAmount,
		Provider: "yipay_v1", ChannelID: 9, ClientIP: "1.2.3.4",
	})
	if err == nil {
		t.Fatal("跨渠道回调应当被拒绝，但成功了")
	}
	if code := api.AsError(err).Code; code != api.CodePaymentChannelMismatch {
		t.Fatalf("期望错误码 %d，实际 %d (%v)", api.CodePaymentChannelMismatch, code, err)
	}

	got, err := env.repos.Order.FindByNo(env.ctx, nil, order.OrderNo)
	if err != nil {
		t.Fatal(err)
	}
	if got.IsPaidLike() {
		t.Fatalf("跨渠道回调后订单不应变为已支付，实际状态 %s", got.Status)
	}

	// 正确渠道的回调仍然照常工作
	if _, err := env.svc.Order.HandlePaymentSuccess(env.ctx, PaymentSuccessInput{
		OrderNo: order.OrderNo, TradeNo: "T-OK", Amount: order.PayAmount,
		Provider: "yipay_v1", ChannelID: 7, ClientIP: "1.2.3.4",
	}); err != nil {
		t.Fatalf("同渠道回调应当成功: %v", err)
	}
}

// TestNotifyAllowedWhenOrderHasNoChannel 订单尚未选择渠道时不做归属限制，
// 保留管理员测试与线下补单的路径。
func TestNotifyAllowedWhenOrderHasNoChannel(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()
	p := env.seedProduct(cat.ID, model.DeliveryManual, 5000, 100)
	res := env.mustCreateOrder(p.ID, 1, "buyer@example.com", "")

	if _, err := env.svc.Order.HandlePaymentSuccess(env.ctx, PaymentSuccessInput{
		OrderNo: res.Order.OrderNo, TradeNo: "T1", Amount: res.Order.PayAmount,
		Provider: "test", ChannelID: 3,
	}); err != nil {
		t.Fatalf("订单未绑定渠道时应当放行: %v", err)
	}
}

// TestZeroPayableOrderRejected 验证优惠后应付为 0 的订单会被拒绝。
//
// 0 元订单没有任何支付网关受理，但库存已被预占，
// 结果是一张永远付不掉、还占着卡密直到超时的僵尸单。
func TestZeroPayableOrderRejected(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()
	p := env.seedProduct(cat.ID, model.DeliveryAuto, 5000, 0)
	env.seedCodes(p.ID, 10)

	// 面额 50 元的满减券，正好等于商品价格
	if _, err := env.svc.Coupon.Create(env.ctx, &CouponInput{
		Code: "ZEROOUT", Name: "立减50", Type: model.CouponFixed,
		Value: 5000, Scope: model.CouponScopeAll, UsageLimit: 100,
		Status: model.StatusActive,
	}); err != nil {
		t.Fatalf("创建优惠券失败: %v", err)
	}

	before := env.availableCodes(p.ID)

	if _, err := env.createOrder(p.ID, 1, "buyer@example.com", "ZEROOUT"); err == nil {
		t.Fatal("应付金额为 0 的订单应当被拒绝，但创建成功了")
	}

	if after := env.availableCodes(p.ID); after != before {
		t.Fatalf("被拒绝的订单不应占用库存：下单前 %d，下单后 %d", before, after)
	}
}

// TestProductUpdateKeepsStockWhenOmitted 验证更新商品时不传 stock 不会清空库存。
//
// Stock 若用值类型，调用方只想改商品名、请求里没带 stock，
// Go 的零值就会把库存直接写成 0，商品瞬间变成已售罄。
func TestProductUpdateKeepsStockWhenOmitted(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()

	var initial int64 = 250
	p, err := env.svc.Product.Create(env.ctx, &ProductInput{
		CategoryID: cat.ID, Name: "手动发货商品", Slug: "manual-keep-stock",
		Price: 1000, Stock: &initial, DeliveryType: model.DeliveryManual,
		Status: model.ProductStatusOn, MinQuantity: 1, MaxQuantity: 10,
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}

	// 只改名字，完全不提交 stock
	updated, err := env.svc.Product.Update(env.ctx, p.ID, &ProductInput{
		CategoryID: cat.ID, Name: "改了个名字", Slug: "manual-keep-stock",
		Price: 1000, DeliveryType: model.DeliveryManual,
		Status: model.ProductStatusOn, MinQuantity: 1, MaxQuantity: 10,
	})
	if err != nil {
		t.Fatalf("更新商品失败: %v", err)
	}
	if updated.Stock != initial {
		t.Fatalf("未提交 stock 时应保留原库存 %d，实际变成 %d", initial, updated.Stock)
	}

	// 显式提交 0 时才真的清零
	var zero int64
	updated, err = env.svc.Product.Update(env.ctx, p.ID, &ProductInput{
		CategoryID: cat.ID, Name: "改了个名字", Slug: "manual-keep-stock",
		Price: 1000, Stock: &zero, DeliveryType: model.DeliveryManual,
		Status: model.ProductStatusOn, MinQuantity: 1, MaxQuantity: 10,
	})
	if err != nil {
		t.Fatalf("更新商品失败: %v", err)
	}
	if updated.Stock != 0 {
		t.Fatalf("显式提交 stock=0 时应清零，实际 %d", updated.Stock)
	}
}

// TestSecretMaskingRoundTrip 验证脱敏值回传不会覆盖真实密钥。
func TestSecretMaskingRoundTrip(t *testing.T) {
	const real = "sk_live_super_secret_value"

	masked := utils.MaskSecret(real)
	if masked == real {
		t.Fatal("脱敏后不应等于原值")
	}
	if !utils.IsSecretUnchanged(masked) {
		t.Fatalf("脱敏值 %q 应被识别为「未修改」", masked)
	}
	if utils.IsSecretUnchanged("brand_new_key_value") {
		t.Fatal("正常的新密钥不应被识别为「未修改」")
	}
	// 空值表示"清空"，不是"未修改"
	if utils.IsSecretUnchanged("") {
		t.Fatal("空值应当表示清空，而不是保留原值")
	}
}
