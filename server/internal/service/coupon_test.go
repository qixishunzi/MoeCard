package service

import (
	"testing"
	"time"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/utils"
)

// TestCouponCalcDiscount 覆盖优惠计算的所有分支（纯函数，不需要数据库）。
func TestCouponCalcDiscount(t *testing.T) {
	cases := []struct {
		name   string
		coupon model.Coupon
		amount int64
		want   int64
	}{
		{"满减 ¥20", model.Coupon{Type: model.CouponFixed, Value: 2000}, 10000, 2000},
		{"满减额超过订单金额时按订单金额封顶",
			model.Coupon{Type: model.CouponFixed, Value: 20000}, 10000, 10000},
		{"9 折", model.Coupon{Type: model.CouponPercent, Value: 9000}, 10000, 1000},
		{"95 折", model.Coupon{Type: model.CouponPercent, Value: 9500}, 10000, 500},
		{"88 折", model.Coupon{Type: model.CouponPercent, Value: 8800}, 9900, 1188},
		{"折扣受最大优惠限制",
			model.Coupon{Type: model.CouponPercent, Value: 5000, MaxDiscount: 1000}, 10000, 1000},
		{"最大优惠未触发时按折扣算",
			model.Coupon{Type: model.CouponPercent, Value: 9000, MaxDiscount: 5000}, 10000, 1000},
		{"免单（0 折）", model.Coupon{Type: model.CouponPercent, Value: 0}, 10000, 10000},
		{"不打折（10000）", model.Coupon{Type: model.CouponPercent, Value: 10000}, 10000, 0},
		{"金额为 0", model.Coupon{Type: model.CouponFixed, Value: 2000}, 0, 0},
		{"未知类型不给优惠", model.Coupon{Type: "unknown", Value: 2000}, 10000, 0},
		// 整数除法向下取整：宁可少优惠 1 分，也不能多给
		{"折扣除不尽时向下取整",
			model.Coupon{Type: model.CouponPercent, Value: 9999}, 999, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.coupon.CalcDiscount(tc.amount)
			if got != tc.want {
				t.Errorf("CalcDiscount(%d) = %d，期望 %d", tc.amount, got, tc.want)
			}
			if got < 0 {
				t.Error("优惠金额不能为负")
			}
			if got > tc.amount && tc.amount > 0 {
				t.Error("优惠金额不能超过订单金额（折后金额不能为负）")
			}
		})
	}
}

// TestCouponValidation 覆盖优惠券校验链的每一个失败分支。
func TestCouponValidation(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()
	productA := env.seedProduct(cat.ID, model.DeliveryManual, 10000, 100)
	productB := env.seedProduct(cat.ID, model.DeliveryManual, 10000, 100)

	now := utils.NowUTC()
	past := now.Add(-48 * time.Hour)
	future := now.Add(48 * time.Hour)

	t.Run("券不存在", func(t *testing.T) {
		_, err := env.svc.Coupon.Validate(env.ctx, nil, "NOT-EXIST", productA.ID, 10000, "a@b.com")
		assertCode(t, err, api.CodeCouponInvalid)
	})

	t.Run("券已停用", func(t *testing.T) {
		c := mustCreateCoupon(t, env, &CouponInput{
			Code: "DISABLED", Type: model.CouponFixed, Value: 1000,
			Scope: model.CouponScopeAll, Status: model.StatusDisabled,
		})
		_, err := env.svc.Coupon.Validate(env.ctx, nil, c.Code, productA.ID, 10000, "a@b.com")
		assertCode(t, err, api.CodeCouponDisabled)
	})

	t.Run("券未开始", func(t *testing.T) {
		c := mustCreateCoupon(t, env, &CouponInput{
			Code: "NOTSTART", Type: model.CouponFixed, Value: 1000,
			Scope: model.CouponScopeAll, Status: model.StatusActive,
			StartAt: future.Format(time.RFC3339),
		})
		_, err := env.svc.Coupon.Validate(env.ctx, nil, c.Code, productA.ID, 10000, "a@b.com")
		assertCode(t, err, api.CodeCouponNotStarted)
	})

	t.Run("券已过期", func(t *testing.T) {
		c := mustCreateCoupon(t, env, &CouponInput{
			Code: "EXPIRED", Type: model.CouponFixed, Value: 1000,
			Scope: model.CouponScopeAll, Status: model.StatusActive,
			StartAt:  past.Add(-24 * time.Hour).Format(time.RFC3339),
			ExpireAt: past.Format(time.RFC3339),
		})
		_, err := env.svc.Coupon.Validate(env.ctx, nil, c.Code, productA.ID, 10000, "a@b.com")
		assertCode(t, err, api.CodeCouponExpired)
	})

	t.Run("券已领完", func(t *testing.T) {
		c := mustCreateCoupon(t, env, &CouponInput{
			Code: "USEDUP", Type: model.CouponFixed, Value: 1000,
			Scope: model.CouponScopeAll, Status: model.StatusActive, UsageLimit: 1,
		})
		// 直接把 used_count 顶到上限
		if err := env.db.DB.Exec("UPDATE coupons SET used_count = 1 WHERE id = ?", c.ID).Error; err != nil {
			t.Fatal(err)
		}
		_, err := env.svc.Coupon.Validate(env.ctx, nil, c.Code, productA.ID, 10000, "a@b.com")
		assertCode(t, err, api.CodeCouponUsedUp)
	})

	t.Run("指定商品券_不适用其他商品", func(t *testing.T) {
		c := mustCreateCoupon(t, env, &CouponInput{
			Code: "ONLYA", Type: model.CouponFixed, Value: 1000,
			Scope: model.CouponScopeProducts, ProductIDs: []uint64{productA.ID},
			Status: model.StatusActive,
		})
		// 对 A 商品有效
		if _, err := env.svc.Coupon.Validate(env.ctx, nil, c.Code, productA.ID, 10000, "a@b.com"); err != nil {
			t.Errorf("指定商品券对目标商品应当有效，却报错: %v", err)
		}
		// 对 B 商品无效
		_, err := env.svc.Coupon.Validate(env.ctx, nil, c.Code, productB.ID, 10000, "a@b.com")
		assertCode(t, err, api.CodeCouponNotApplicable)
	})

	t.Run("全场券_对所有商品有效", func(t *testing.T) {
		c := mustCreateCoupon(t, env, &CouponInput{
			Code: "ALLPROD", Type: model.CouponFixed, Value: 1000,
			Scope: model.CouponScopeAll, Status: model.StatusActive,
		})
		for _, pid := range []uint64{productA.ID, productB.ID} {
			if _, err := env.svc.Coupon.Validate(env.ctx, nil, c.Code, pid, 10000, "a@b.com"); err != nil {
				t.Errorf("全场券对商品 %d 应当有效，却报错: %v", pid, err)
			}
		}
	})

	t.Run("未达最低消费门槛", func(t *testing.T) {
		c := mustCreateCoupon(t, env, &CouponInput{
			Code: "MIN100", Type: model.CouponFixed, Value: 2000, MinAmount: 10000,
			Scope: model.CouponScopeAll, Status: model.StatusActive,
		})
		_, err := env.svc.Coupon.Validate(env.ctx, nil, c.Code, productA.ID, 9999, "a@b.com")
		assertCode(t, err, api.CodeCouponMinAmount)

		// 刚好达到门槛应当通过
		res, err := env.svc.Coupon.Validate(env.ctx, nil, c.Code, productA.ID, 10000, "a@b.com")
		if err != nil {
			t.Fatalf("刚好达到门槛时应当通过，却报错: %v", err)
		}
		if res.DiscountAmount != 2000 || res.PayAmount != 8000 {
			t.Errorf("满减计算错误: discount=%d pay=%d", res.DiscountAmount, res.PayAmount)
		}
	})

	t.Run("超过个人使用次数", func(t *testing.T) {
		c := mustCreateCoupon(t, env, &CouponInput{
			Code: "PERUSER1", Type: model.CouponFixed, Value: 1000,
			Scope: model.CouponScopeAll, Status: model.StatusActive, PerUserLimit: 1,
		})
		// 造一条已核销记录
		if err := env.repos.Coupon.CreateUsage(env.ctx, nil, &model.CouponUsage{
			CouponID: c.ID, OrderID: 99999, Email: "used@example.com", DiscountAmount: 1000,
		}); err != nil {
			t.Fatal(err)
		}
		_, err := env.svc.Coupon.Validate(env.ctx, nil, c.Code, productA.ID, 10000, "used@example.com")
		assertCode(t, err, api.CodeCouponUserLimit)

		// 另一个邮箱不受影响
		if _, err := env.svc.Coupon.Validate(env.ctx, nil, c.Code, productA.ID, 10000, "other@example.com"); err != nil {
			t.Errorf("其他用户应当仍可使用，却报错: %v", err)
		}
	})

	t.Run("邮箱大小写不影响个人限次", func(t *testing.T) {
		c := mustCreateCoupon(t, env, &CouponInput{
			Code: "CASETEST", Type: model.CouponFixed, Value: 1000,
			Scope: model.CouponScopeAll, Status: model.StatusActive, PerUserLimit: 1,
		})
		if err := env.repos.Coupon.CreateUsage(env.ctx, nil, &model.CouponUsage{
			CouponID: c.ID, OrderID: 99998, Email: "Case@Example.COM", DiscountAmount: 1000,
		}); err != nil {
			t.Fatal(err)
		}
		// 用不同大小写再试，必须仍然被拦住
		_, err := env.svc.Coupon.Validate(env.ctx, nil, c.Code, productA.ID, 10000, "case@example.com")
		assertCode(t, err, api.CodeCouponUserLimit)
	})

	t.Run("券码大小写不敏感", func(t *testing.T) {
		mustCreateCoupon(t, env, &CouponInput{
			Code: "LOWERCASE", Type: model.CouponFixed, Value: 1000,
			Scope: model.CouponScopeAll, Status: model.StatusActive,
		})
		if _, err := env.svc.Coupon.Validate(env.ctx, nil, "lowercase", productA.ID, 10000, "a@b.com"); err != nil {
			t.Errorf("券码应当大小写不敏感，却报错: %v", err)
		}
	})
}

// TestCouponNotConsumedOnOrderCreate 验证创建未支付订单不会消耗优惠券额度。
//
// 这是防刷单的关键：否则攻击者可以狂建订单把限量券刷光。
func TestCouponNotConsumedOnOrderCreate(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()
	product := env.seedProduct(cat.ID, model.DeliveryManual, 10000, 1000)

	c := mustCreateCoupon(t, env, &CouponInput{
		Code: "LIMIT3", Type: model.CouponFixed, Value: 1000,
		Scope: model.CouponScopeAll, Status: model.StatusActive, UsageLimit: 3,
	})

	// 建 10 个未支付订单
	for i := 0; i < 10; i++ {
		if _, err := env.createOrder(product.ID, 1, "spammer@example.com", c.Code); err != nil {
			t.Fatalf("第 %d 单创建失败: %v", i+1, err)
		}
	}

	after, err := env.repos.Coupon.FindByID(env.ctx, nil, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.UsedCount != 0 {
		t.Errorf("创建未支付订单不应消耗优惠券额度，当前 used_count = %d", after.UsedCount)
	}
}

// TestCouponRedeemOnPayment 验证优惠券在支付成功时才核销。
func TestCouponRedeemOnPayment(t *testing.T) {
	env := newTestEnv(t)
	cat := env.seedCategory()
	product := env.seedProduct(cat.ID, model.DeliveryManual, 10000, 100)

	c := mustCreateCoupon(t, env, &CouponInput{
		Code: "REDEEM", Type: model.CouponPercent, Value: 8000, // 8 折
		Scope: model.CouponScopeAll, Status: model.StatusActive, UsageLimit: 5,
	})

	order := env.mustCreateOrder(product.ID, 1, "buyer@example.com", c.Code)
	if order.Order.PayAmount != 8000 {
		t.Fatalf("8 折后应付 8000，实际 %d", order.Order.PayAmount)
	}

	if _, err := env.pay(order.Order.OrderNo, "T1", order.Order.PayAmount); err != nil {
		t.Fatalf("支付失败: %v", err)
	}

	after, _ := env.repos.Coupon.FindByID(env.ctx, nil, c.ID)
	if after.UsedCount != 1 {
		t.Errorf("支付成功后应核销 1 次，实际 %d 次", after.UsedCount)
	}

	// 手动发货商品支付后进入待发货
	o, _, _ := env.svc.Order.GetByNo(env.ctx, order.Order.OrderNo)
	if o.Status != model.OrderWaitingDelivery {
		t.Errorf("手动发货商品支付后应为 waiting_delivery，实际 %s", o.Status)
	}
}

func assertCode(t *testing.T, err error, want api.ErrCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望错误码 %d，实际没有报错", want)
	}
	if got := api.AsError(err).Code; got != want {
		t.Errorf("期望错误码 %d，实际 %d（%v）", want, got, err)
	}
}
