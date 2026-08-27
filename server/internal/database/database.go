// Package database 封装数据库连接、事务与驱动差异。
//
// 这是整个项目里**唯一**允许出现 "if sqlite / if mysql" 的地方。
// 业务层只使用 DB.Tx / DB.LockOrder 等统一 API。
//
// 两个驱动的核心差异：
//
//	SQLite：单写者模型。多个并发写事务会得到 SQLITE_BUSY。
//	        对策 = 进程内全局写互斥 + BEGIN IMMEDIATE + busy_timeout。
//	        行级 FOR UPDATE 不存在，但因为写事务被串行化，效果等价。
//	MySQL： 真正的行锁。用 SELECT ... FOR UPDATE 锁订单行，
//	        并发写事务可以并行推进，遇到死锁时重试。
package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"

	"github.com/moecard/server/internal/config"
	"github.com/moecard/server/internal/logger"
)

// DB 是对 *gorm.DB 的封装，附带驱动信息与写锁。
type DB struct {
	*gorm.DB
	driver string

	// writeMu 仅在 SQLite 下使用：把所有写事务串行化，
	// 从根本上消除 SQLITE_BUSY 与写-写竞争。
	writeMu sync.Mutex
}

// Open 建立数据库连接。
func Open(cfg *config.Config) (*DB, error) {
	gcfg := &gorm.Config{
		Logger: newGormLogger(cfg.Database.SlowThreshold, cfg.IsProduction()),
		// 时间统一用 UTC 写入，展示层再按商城时区转换。
		NowFunc:                                  func() time.Time { return time.Now().UTC() },
		DisableForeignKeyConstraintWhenMigrating: true,
		SkipDefaultTransaction:                   true, // 事务边界由 service 显式控制
	}

	var (
		gdb *gorm.DB
		err error
	)
	dsn := cfg.DSN()

	switch cfg.Database.Driver {
	case config.DriverMySQL:
		gdb, err = gorm.Open(mysql.New(mysql.Config{
			DSN:                       dsn,
			DefaultStringSize:         191, // 兼容 utf8mb4 + InnoDB 索引长度限制
			DontSupportRenameIndex:    true,
			DontSupportRenameColumn:   true,
			SkipInitializeWithVersion: false,
		}), gcfg)
	case config.DriverSQLite:
		gdb, err = gorm.Open(sqlite.Open(dsn), gcfg)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", cfg.Database.Driver)
	}
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}

	if cfg.Database.Driver == config.DriverSQLite {
		// SQLite 只有一个写者；连接池开大只会制造 BUSY，没有任何收益。
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
	} else {
		sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
		sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
		sqlDB.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)
	}

	// 连接重试：容器编排下 MySQL 往往比应用晚几秒就绪，
	// 直接失败退出会让容器反复重启。这里给它一段合理的等待窗口。
	// 生产环境中数据库短暂重启时同样受益。
	if err := pingWithRetry(sqlDB, cfg.Database.Driver); err != nil {
		return nil, err
	}

	logger.L().Info("database connected", "driver", cfg.Database.Driver)
	return &DB{DB: gdb, driver: cfg.Database.Driver}, nil
}

// pingWithRetry 带退避重试的连通性检查。
func pingWithRetry(sqlDB interface {
	PingContext(context.Context) error
}, driver string) error {
	const (
		maxAttempts = 10
		baseDelay   = 1500 * time.Millisecond
	)

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := sqlDB.PingContext(ctx)
		cancel()
		if err == nil {
			if attempt > 1 {
				logger.L().Info("database reachable after retry", "attempts", attempt)
			}
			return nil
		}
		lastErr = err

		// SQLite 连不上是本地文件/权限问题，重试没有意义，直接失败更容易排查
		if driver == config.DriverSQLite {
			break
		}
		if attempt < maxAttempts {
			logger.L().Warn("database not ready, retrying",
				"attempt", attempt, "max", maxAttempts, "err", err)
			time.Sleep(baseDelay)
		}
	}
	return fmt.Errorf("ping database: %w", lastErr)
}

// Driver 返回当前驱动名。
func (d *DB) Driver() string { return d.driver }

// IsSQLite 判断是否 SQLite。仅供 database 包内部与迁移器使用。
func (d *DB) IsSQLite() bool { return d.driver == config.DriverSQLite }

// Close 关闭连接。
func (d *DB) Close() error {
	sqlDB, err := d.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

const (
	maxTxRetries = 3
	retryBackoff = 60 * time.Millisecond
)

// Tx 在事务中执行 fn，并自动处理驱动差异与可重试错误。
//
// - SQLite：全局写互斥，保证同一时刻只有一个写事务；
// - MySQL：遇到死锁 / 锁等待超时自动重试；
// - fn 返回 error 或 panic 时回滚。
//
// 注意：fn 内部**禁止**再调用 d.Tx（会自锁）。嵌套请直接使用传入的 *gorm.DB。
func (d *DB) Tx(ctx context.Context, fn func(tx *gorm.DB) error) error {
	if d.IsSQLite() {
		d.writeMu.Lock()
		defer d.writeMu.Unlock()
	}

	var lastErr error
	for attempt := 0; attempt < maxTxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * retryBackoff):
			}
			logger.L().Warn("retrying transaction", "attempt", attempt+1, "err", lastErr)
		}

		err := d.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return fn(tx)
		})
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRetryable(err) {
			return err
		}
	}
	return fmt.Errorf("transaction failed after %d attempts: %w", maxTxRetries, lastErr)
}

// isRetryable 判断错误是否值得重试（死锁 / 锁超时 / SQLite busy）。
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, sig := range []string{
		"deadlock",           // MySQL 1213
		"lock wait timeout",  // MySQL 1205
		"database is locked", // SQLite SQLITE_BUSY
		"database table is locked",
		"sqlite_busy",
	} {
		if strings.Contains(msg, sig) {
			return true
		}
	}
	return false
}

// LockForUpdate 给查询加行锁。
//
// MySQL 走真正的 SELECT ... FOR UPDATE；
// SQLite 返回原查询 —— 因为 Tx 已经通过全局写互斥把写事务串行化了，
// 再加锁语句只会导致语法错误。业务层无需关心这个差异。
func (d *DB) LockForUpdate(tx *gorm.DB) *gorm.DB {
	if d.IsSQLite() {
		return tx
	}
	return tx.Clauses(clause.Locking{Strength: "UPDATE"})
}

// WithWriteLock 在非事务场景下执行需要串行化的写操作（SQLite 专用保护）。
func (d *DB) WithWriteLock(fn func() error) error {
	if d.IsSQLite() {
		d.writeMu.Lock()
		defer d.writeMu.Unlock()
	}
	return fn()
}

// IsNotFound 是 gorm.ErrRecordNotFound 的语义化判断。
func IsNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }

// IsDuplicate 判断唯一约束冲突（跨驱动）。
func IsDuplicate(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate entry") || // MySQL
		strings.Contains(msg, "unique constraint") || // SQLite
		strings.Contains(msg, "unique_violation")
}

// ---- GORM 日志适配到 slog ----

type gormSlogLogger struct {
	slowThreshold time.Duration
	level         gormlogger.LogLevel
}

func newGormLogger(slow time.Duration, production bool) gormlogger.Interface {
	if slow <= 0 {
		slow = 300 * time.Millisecond
	}
	lvl := gormlogger.Warn
	if !production {
		lvl = gormlogger.Warn // SQL 明细太吵；需要时把 LOG_LEVEL 设为 debug 并改这里为 Info
	}
	return &gormSlogLogger{slowThreshold: slow, level: lvl}
}

func (l *gormSlogLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	nl := *l
	nl.level = level
	return &nl
}

func (l *gormSlogLogger) Info(ctx context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Info {
		logger.FromContext(ctx).Info(fmt.Sprintf(msg, args...), "scope", "sql")
	}
}

func (l *gormSlogLogger) Warn(ctx context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Warn {
		logger.FromContext(ctx).Warn(fmt.Sprintf(msg, args...), "scope", "sql")
	}
}

func (l *gormSlogLogger) Error(ctx context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Error {
		logger.FromContext(ctx).Error(fmt.Sprintf(msg, args...), "scope", "sql")
	}
}

func (l *gormSlogLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level <= gormlogger.Silent {
		return
	}
	elapsed := time.Since(begin)
	log := logger.FromContext(ctx)

	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && l.level >= gormlogger.Error:
		sql, rows := fc()
		log.Error("sql error", "err", err, "elapsed_ms", elapsed.Milliseconds(), "rows", rows, "sql", truncate(sql, 500))
	case elapsed > l.slowThreshold && l.level >= gormlogger.Warn:
		sql, rows := fc()
		log.Warn("slow sql", "elapsed_ms", elapsed.Milliseconds(), "rows", rows, "sql", truncate(sql, 500))
	case l.level >= gormlogger.Info:
		sql, rows := fc()
		log.Debug("sql", "elapsed_ms", elapsed.Milliseconds(), "rows", rows, "sql", truncate(sql, 500))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

var _ = slog.LevelInfo
