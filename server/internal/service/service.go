package service

import (
	"context"
	"sync"
	"time"

	"github.com/moecard/server/internal/config"
	"github.com/moecard/server/internal/database"
	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/storage"
)

// Services 聚合所有服务，便于在 handler 层依赖注入。
type Services struct {
	Setting   *SettingService
	Category  *CategoryService
	Product   *ProductService
	Code      *CodeService
	Coupon    *CouponService
	Order     *OrderService
	Payment   *PaymentService
	Mail      *MailService
	Notify    *NotifyService
	Admin     *AdminService
	Dashboard *DashboardService
	Storage   storage.Provider

	scheduler *Scheduler
}

// New 构造全部服务并完成依赖装配。
func New(cfg *config.Config, db *database.DB, repos *repository.Repositories) (*Services, error) {
	store, err := storage.NewLocal(cfg.Storage.LocalPath, cfg.Storage.URLPrefix, cfg.Storage.MaxSize)
	if err != nil {
		return nil, err
	}

	settings := NewSettingService(db, repos.Setting)
	if err := settings.EnsureDefaults(context.Background()); err != nil {
		return nil, err
	}

	frontendURL := cfg.App.FrontendURL
	if frontendURL == "" {
		frontendURL = cfg.App.BaseURL
	}
	baseURL := cfg.App.BaseURL
	if baseURL == "" {
		// 开发环境未配置时用本地地址兜底；生产环境 config.Validate 已强制要求配置
		baseURL = "http://127.0.0.1:" + itoa(cfg.App.Port)
		logger.L().Warn("未配置 BASE_URL，支付异步通知地址将使用本地地址（仅适用于开发环境）", "base_url", baseURL)
	}
	if frontendURL == "" {
		frontendURL = baseURL
	}

	mailer := NewMailService(db, repos.Log, settings, frontendURL)
	notifier := NewNotifyService(settings, repos.Notify, mailer, baseURL)
	coupon := NewCouponService(db, repos.Coupon, repos.Product)
	order := NewOrderService(db, repos, coupon, settings, mailer, notifier)
	pay := NewPaymentService(db, repos.Payment, order, settings, notifier, baseURL, frontendURL)

	s := &Services{
		Setting:   settings,
		Category:  NewCategoryService(db, repos.Category, repos.Product),
		Product:   NewProductService(db, repos, settings, notifier),
		Code:      NewCodeService(db, repos),
		Coupon:    coupon,
		Order:     order,
		Payment:   pay,
		Mail:      mailer,
		Notify:    notifier,
		Admin:     NewAdminService(db, repos, settings, cfg.App.JWTSecret, cfg.App.JWTExpire),
		Dashboard: NewDashboardService(repos, settings),
		Storage:   store,
	}
	s.scheduler = NewScheduler(s, repos)
	return s, nil
}

// StartBackgroundJobs 启动后台定时任务。
func (s *Services) StartBackgroundJobs(ctx context.Context) { s.scheduler.Start(ctx) }

// StopBackgroundJobs 停止后台定时任务并等待退出。
func (s *Services) StopBackgroundJobs() { s.scheduler.Stop() }

// Scheduler 负责周期性后台任务。
//
// 为什么不用 cron 库：只有三个固定周期的任务，
// time.Ticker + goroutine 就够了，少一个依赖。
type Scheduler struct {
	svc   *Services
	repos *repository.Repositories

	cancel context.CancelFunc
	wg     sync.WaitGroup
	once   sync.Once
}

// NewScheduler 构造。
func NewScheduler(svc *Services, repos *repository.Repositories) *Scheduler {
	return &Scheduler{svc: svc, repos: repos}
}

// Start 启动所有定时任务。
func (s *Scheduler) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel

	// 订单过期扫描：每分钟一次。
	// 这是"预占库存"模型能成立的前提 —— 没有它，未支付订单会永久占用库存。
	s.run(ctx, "expire-orders", time.Minute, func(ctx context.Context) {
		if _, err := s.svc.Order.ExpireDueOrders(ctx, 200); err != nil {
			logger.L().Error("处理过期订单失败", "err", err)
		}
	})

	// 孤儿卡密回收：每 10 分钟一次。
	// 进程在"锁定卡密"与"写订单状态"之间崩溃时会留下孤儿锁定，这是兜底清理。
	s.run(ctx, "release-stale-locks", 10*time.Minute, func(ctx context.Context) {
		if _, err := s.svc.Order.ReleaseStaleCodeLocks(ctx, 2*time.Hour); err != nil {
			logger.L().Error("释放孤儿卡密锁定失败", "err", err)
		}
	})

	// 库存告警扫描：每 15 分钟一次。
	// 不在下单路径上实时判断 —— 那会给热路径加一次统计查询，
	// 而库存告急晚知道十几分钟并不影响处置。
	s.run(ctx, "low-stock-scan", 15*time.Minute, func(ctx context.Context) {
		if err := s.svc.Product.ScanLowStock(ctx); err != nil {
			logger.L().Error("库存告警扫描失败", "err", err)
		}
	})

	// 日志清理：每 24 小时一次，保留 180 天。
	s.run(ctx, "purge-logs", 24*time.Hour, func(ctx context.Context) {
		before := time.Now().UTC().AddDate(0, 0, -180)
		n, err := s.repos.Log.PurgeOldLogs(ctx, nil, before)
		if err != nil {
			logger.L().Error("清理过期日志失败", "err", err)
			return
		}
		if m, err := s.repos.Notify.Purge(ctx, nil, before); err == nil {
			n += m
		}
		if n > 0 {
			logger.L().Info("已清理过期日志", "count", n)
		}
	})

	logger.L().Info("后台定时任务已启动")
}

func (s *Scheduler) run(ctx context.Context, name string, interval time.Duration, fn func(context.Context)) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// 启动后立刻执行一次，不用等第一个 tick
		s.safeRun(ctx, name, fn)
		for {
			select {
			case <-ctx.Done():
				logger.L().Debug("定时任务已停止", "job", name)
				return
			case <-ticker.C:
				s.safeRun(ctx, name, fn)
			}
		}
	}()
}

// safeRun 捕获任务 panic，避免一个任务崩掉整个进程。
func (s *Scheduler) safeRun(ctx context.Context, name string, fn func(context.Context)) {
	defer func() {
		if r := recover(); r != nil {
			logger.L().Error("定时任务 panic", "job", name, "panic", r)
		}
	}()
	// 单次任务超时保护，防止某次执行卡死后续所有 tick
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	fn(runCtx)
}

// Stop 停止所有任务。
func (s *Scheduler) Stop() {
	s.once.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		s.wg.Wait()
		logger.L().Info("后台定时任务已停止")
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
