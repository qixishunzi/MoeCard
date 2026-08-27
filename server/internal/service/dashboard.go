package service

import (
	"context"
	"fmt"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/utils"
)

// DashboardService 提供后台首页统计。
type DashboardService struct {
	orders   *repository.OrderRepo
	products *repository.ProductRepo
	codes    *repository.CodeRepo
	settings *SettingService
}

// NewDashboardService 构造。
func NewDashboardService(repos *repository.Repositories, settings *SettingService) *DashboardService {
	return &DashboardService{
		orders: repos.Order, products: repos.Product, codes: repos.Code, settings: settings,
	}
}

// DashboardStats 是 Dashboard 的统计数据。
type DashboardStats struct {
	TodayOrders      int64 `json:"today_orders"`
	TodayRevenue     int64 `json:"today_revenue"`
	YesterdayRevenue int64 `json:"yesterday_revenue"`
	TotalRevenue     int64 `json:"total_revenue"`
	MonthRevenue     int64 `json:"month_revenue"`

	PendingOrders   int64 `json:"pending_orders"`
	PaidOrders      int64 `json:"paid_orders"`
	WaitingDelivery int64 `json:"waiting_delivery"`
	CompletedOrders int64 `json:"completed_orders"`
	TotalOrders     int64 `json:"total_orders"`

	ProductCount   int64 `json:"product_count"`
	OnSaleCount    int64 `json:"on_sale_count"`
	CodeStock      int64 `json:"code_stock"`
	NeedsAttention int64 `json:"needs_attention"`

	RecentOrders []RecentOrder `json:"recent_orders"`
}

// RecentOrder 是 Dashboard「最近订单」的出参。
//
// 单独定义 DTO 而不是直接返回 model.Order：
//   - 需要 status_text（状态中文），那是展示层概念，不属于 Model
//   - created_at 要按商城时区格式化，直接返回 UTC 会让管理员看到错位 8 小时的时间
//   - 邮箱要脱敏
type RecentOrder struct {
	ID          uint64 `json:"id"`
	OrderNo     string `json:"order_no"`
	Email       string `json:"email"`
	ProductName string `json:"product_name"`
	Quantity    int    `json:"quantity"`
	PayAmount   int64  `json:"pay_amount"`
	Status      string `json:"status"`
	StatusText  string `json:"status_text"`
	CreatedAt   string `json:"created_at"`
}

// Stats 汇总 Dashboard 数据。
//
// 时间切分按**商城配置的时区**进行 —— 数据库存的是 UTC，
// 若直接按 UTC 分日，UTC+8 的商家会看到"今天"从早上 8 点才开始。
func (s *DashboardService) Stats(ctx context.Context) (*DashboardStats, error) {
	tz := s.settings.Timezone()
	now := utils.NowUTC()

	todayStart, todayEnd := utils.DayRangeUTC(now, tz)
	yStart, yEnd := utils.DayRangeUTC(now.AddDate(0, 0, -1), tz)
	monthStart := todayStart.AddDate(0, 0, -29)

	out := &DashboardStats{}

	var err error
	if out.TodayRevenue, out.TodayOrders, err = s.orders.SumRevenue(ctx, nil, &todayStart, &todayEnd); err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	if out.YesterdayRevenue, _, err = s.orders.SumRevenue(ctx, nil, &yStart, &yEnd); err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	if out.MonthRevenue, _, err = s.orders.SumRevenue(ctx, nil, &monthStart, &todayEnd); err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	if out.TotalRevenue, _, err = s.orders.SumRevenue(ctx, nil, nil, nil); err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}

	counts, err := s.orders.CountByStatus(ctx, nil, nil)
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	out.PendingOrders = counts[model.OrderPending] + counts[model.OrderPaying]
	out.PaidOrders = counts[model.OrderPaid]
	out.WaitingDelivery = counts[model.OrderWaitingDelivery]
	out.CompletedOrders = counts[model.OrderCompleted]
	for _, v := range counts {
		out.TotalOrders += v
	}

	if out.ProductCount, err = s.products.Count(ctx, nil, false); err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	if out.OnSaleCount, err = s.products.Count(ctx, nil, true); err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	if out.CodeStock, err = s.codes.TotalUnused(ctx, nil); err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	if out.NeedsAttention, err = s.orders.CountNeedsAttention(ctx, nil); err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}

	recent, err := s.orders.Recent(ctx, nil, 10)
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	out.RecentOrders = make([]RecentOrder, 0, len(recent))
	if len(recent) > 0 {
		ids := make([]uint64, 0, len(recent))
		for _, o := range recent {
			ids = append(ids, o.ID)
		}
		itemMap, _ := s.orders.FindItemsBatch(ctx, nil, ids)

		for _, o := range recent {
			r := RecentOrder{
				ID:      o.ID,
				OrderNo: o.OrderNo,
				// 邮箱脱敏，减少后台截图/投屏时的隐私泄露
				Email:      utils.MaskEmail(o.Email),
				PayAmount:  o.PayAmount,
				Status:     o.Status,
				StatusText: model.OrderStatusLabel(o.Status),
				CreatedAt:  utils.FormatInZone(o.CreatedAt, tz, ""),
			}
			if items := itemMap[o.ID]; len(items) > 0 {
				r.ProductName = items[0].ProductName
				r.Quantity = items[0].Quantity
				if len(items) > 1 {
					r.ProductName += fmt.Sprintf(" 等 %d 件商品", len(items))
				}
			}
			out.RecentOrders = append(out.RecentOrders, r)
		}
	}
	return out, nil
}

// Trend 返回最近 days 天的销售趋势。
func (s *DashboardService) Trend(ctx context.Context, days int) ([]repository.TrendPoint, error) {
	if days != 30 {
		days = 7
	}
	tz := s.settings.Timezone()
	now := utils.NowUTC()
	_, end := utils.DayRangeUTC(now, tz)
	start := end.AddDate(0, 0, -days)

	// 起点不早于系统部署那天。
	//
	// 一个刚上线三天的店，"近 30 天"里有 27 天必然是零 —— 那不是经营数据，
	// 是一条误导人的长平线，还会把真实波动压扁到看不见。
	if installed, err := s.settings.InstalledAt(ctx); err == nil && !installed.IsZero() {
		day := utils.StartOfDayUTC(installed, tz)
		if day.After(start) {
			start = day
		}
	}
	if !start.Before(end) {
		// 同一天部署同一天看：至少给出今天这一个点
		start = utils.StartOfDayUTC(now, tz)
	}

	points, err := s.orders.RevenueByDay(ctx, nil, start, end, tz)
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	return points, nil
}
