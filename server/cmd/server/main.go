// Command server 是 MoeCard 的 API 服务入口。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	// 把 IANA 时区库编进二进制。
	//
	// Windows 没有系统时区库，Go 会退回去找 $GOROOT/lib/time/zoneinfo.zip；
	// 而发布用的 -trimpath 会把 runtime.GOROOT() 清空，那条退路也断了 ——
	// 结果是「Asia/Shanghai」这类名字在打包好的程序里一个都加载不出来：
	// 后台存不了时区，所有时间还会静默按 UTC 显示，差了整整 8 小时。
	// Linux 上换成不带 tzdata 的精简镜像也是同一个坑。
	// 代价是二进制大约多 450KB，换来的是彻底不依赖运行环境。
	_ "time/tzdata"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/config"
	"github.com/moecard/server/internal/database"
	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/internal/middleware"
	_ "github.com/moecard/server/internal/payment/providers" // 注册全部支付渠道
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/router"
	"github.com/moecard/server/internal/service"
	"github.com/moecard/server/internal/utils"
	"github.com/moecard/server/internal/web"
)

// version 由构建时通过 -ldflags "-X main.version=x.y.z" 注入。
// 未注入时回退到代码里的默认版本号。
var version = api.Version

// buildTime 与 gitCommit 同样可选注入，便于线上定位到具体构建产物。
var (
	buildTime = "unknown"
	gitCommit = "unknown"
)

func main() {
	var (
		configPath  = flag.String("config", "", "配置文件路径（默认 config/config.yaml）")
		showVersion = flag.Bool("version", false, "显示版本号")
		skipMigrate = flag.Bool("skip-migrate", false, "跳过启动时的数据库迁移")
		doUpdate    = flag.Bool("update", false, "检查并安装新版本（需要重启生效）")
		installSvc  = flag.Bool("install-service", false, "装成开机自启的 systemd 服务（需要 root）")
		removeSvc   = flag.Bool("uninstall-service", false, "取消开机自启，数据不动")
		printSvc    = flag.Bool("print-service", false, "把 systemd 单元文件打到标准输出")
		checkUpdate = flag.Bool("check-update", false, "只检查有没有新版本，不安装")
		assumeYes   = flag.Bool("y", false, "更新时不再询问确认")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("MoeCard %s\n", version)
		fmt.Printf("  构建时间: %s\n", buildTime)
		fmt.Printf("  Git 提交: %s\n", gitCommit)
		fmt.Printf("  Go 版本:  %s\n", runtime.Version())
		fmt.Printf("  平台:     %s/%s\n", runtime.GOOS, runtime.GOARCH)
		return
	}

	if *printSvc {
		// 固定用默认路径：这条命令是用来生成 scripts/moecard.service
		// 那份参考文件的，得和 service_test.go 里比对的内容一致
		fmt.Print(renderUnit("/opt/moecard/moecard", "/opt/moecard", serviceUser))
		return
	}

	if *installSvc || *removeSvc {
		var err error
		if *installSvc {
			err = installService()
		} else {
			err = uninstallService()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		return
	}

	if *checkUpdate || *doUpdate {
		if err := runUpdate(version, *checkUpdate, *assumeYes); err != nil {
			what := "更新"
			if *checkUpdate {
				what = "检查更新"
			}
			fmt.Fprintf(os.Stderr, "%s失败: %v\n", what, err)
			os.Exit(1)
		}
		return
	}

	if err := run(*configPath, *skipMigrate); err != nil {
		fmt.Fprintf(os.Stderr, "启动失败: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath string, skipMigrate bool) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	if err := logger.Init(logger.Config{
		Level:  cfg.Log.Level,
		Format: cfg.Log.Format,
		File:   cfg.Log.File,
	}); err != nil {
		return fmt.Errorf("初始化日志失败: %w", err)
	}
	defer logger.Close()

	logger.L().Info("MoeCard 启动中",
		"version", version,
		"env", cfg.App.Env,
		"driver", cfg.Database.Driver)

	if !cfg.IsProduction() {
		logger.L().Warn("当前为开发模式。上线前请设置 APP_ENV=production 并配置强 JWT_SECRET")
	}

	// 静态加密必须在任何数据读写之前初始化：
	// 晚一步就可能出现"先按明文写了几条卡密，之后才开始加密"的混合状态。
	if err := utils.InitDataEncryption(cfg.App.DataKey); err != nil {
		return fmt.Errorf("初始化数据加密失败: %w", err)
	}
	if utils.DataEncryptionEnabled() {
		logger.L().Info("卡密与 TOTP 密钥已启用静态加密")
	} else if cfg.IsProduction() {
		logger.L().Warn("未配置 DATA_ENCRYPTION_KEY，卡密将以明文存储 —— " +
			"数据库一旦泄露，未售卡密可被直接使用。建议用 openssl rand -hex 32 生成后配置")
	}

	db, err := database.Open(cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	// 迁移在服务启动前完成：schema 不对就不该开始接受请求
	if !skipMigrate {
		if err := db.Migrate(); err != nil {
			return fmt.Errorf("数据库迁移失败: %w", err)
		}
	}

	repos := repository.New(db)
	svc, err := service.New(cfg, db, repos)
	if err != nil {
		return fmt.Errorf("初始化服务失败: %w", err)
	}

	// 首次启动提示：没有管理员时引导去 /setup
	if need, err := svc.Admin.NeedsSetup(context.Background()); err == nil && need {
		logger.L().Warn("系统尚未初始化，请访问 " + firstNonEmpty(cfg.App.FrontendURL, cfg.App.BaseURL, "http://127.0.0.1:"+itoa(cfg.App.Port)) + "/setup 完成初始化")
	}

	limiters := middleware.NewLimiters()
	defer limiters.Close()

	// 嵌入的前端。加载失败时降级为纯 API 服务（前后端分离部署场景）。
	//
	// 传的是函数不是值：后台入口路径能在运行时改，改完要立刻生效，
	// 不能要求店主重启一次服务才能用上新入口。
	spaHandler, err := web.SPAHandler(svc.Setting.AdminPath)
	if err != nil {
		logger.L().Warn("前端资源加载失败，将只提供 API 服务", "err", err)
		spaHandler = nil
	} else if !web.HasRealBuild() {
		logger.L().Warn("嵌入的是前端占位页面。请先执行 npm run build 再编译后端")
	}

	handler := router.NewRouter(router.RouterDeps{
		Config:   cfg,
		Services: svc,
		Limiters: limiters,
		SPA:      spaHandler,
		Build: api.BuildInfo{
			Version:   version,
			BuildTime: buildTime,
			Commit:    gitCommit,
		},
	})

	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: handler,
		// 超时设置是防御性的：没有它们，慢连接攻击可以耗尽服务器连接数
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	// 后台任务：订单过期、孤儿卡密回收、日志清理
	jobCtx, stopJobs := context.WithCancel(context.Background())
	defer stopJobs()
	svc.StartBackgroundJobs(jobCtx)

	errCh := make(chan error, 1)
	go func() {
		logger.L().Info("HTTP 服务已启动", "addr", cfg.Addr(), "docs", "/api/docs")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// 优雅关闭：先停止接受新请求，等已有请求处理完，再停后台任务。
	// 顺序很重要 —— 反过来会让正在处理的支付回调失去后台服务支撑。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return fmt.Errorf("HTTP 服务异常: %w", err)
	case sig := <-quit:
		logger.L().Info("收到退出信号，开始优雅关闭", "signal", sig.String())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.L().Error("HTTP 服务关闭超时", "err", err)
	}

	stopJobs()
	svc.StopBackgroundJobs()
	logger.L().Info("MoeCard 已安全退出")
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func itoa(n int) string { return fmt.Sprint(n) }
