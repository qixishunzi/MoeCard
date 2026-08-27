// Package logger 基于标准库 log/slog 提供结构化日志。
//
// 分三类 logger（对应架构文档 §37）：
//   - App     应用日志
//   - Payment 支付日志（同时会落 payment_logs 表）
//   - Admin   管理员操作日志（同时会落 admin_operation_logs 表）
//
// 落表由 service 负责；这里只保证文件/stdout 侧有可检索的结构化记录。
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Config struct {
	Level  string
	Format string
	File   string
}

var (
	base    *slog.Logger
	once    sync.Once
	logFile *os.File
)

// Init 初始化全局 logger。多次调用只生效一次。
func Init(cfg Config) error {
	var err error
	once.Do(func() {
		var w io.Writer = os.Stdout
		if cfg.File != "" {
			if e := os.MkdirAll(filepath.Dir(cfg.File), 0o755); e != nil {
				err = fmt.Errorf("create log dir: %w", e)
				return
			}
			f, e := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if e != nil {
				err = fmt.Errorf("open log file: %w", e)
				return
			}
			logFile = f
			w = io.MultiWriter(os.Stdout, f)
		}

		opts := &slog.HandlerOptions{Level: parseLevel(cfg.Level)}
		var h slog.Handler
		if strings.EqualFold(cfg.Format, "json") {
			h = slog.NewJSONHandler(w, opts)
		} else {
			h = slog.NewTextHandler(w, opts)
		}
		base = slog.New(h)
		slog.SetDefault(base)
	})
	return err
}

// Close 关闭日志文件句柄。
func Close() {
	if logFile != nil {
		_ = logFile.Close()
	}
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func root() *slog.Logger {
	if base == nil {
		base = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	return base
}

// L 返回应用日志器。
func L() *slog.Logger { return root().With("scope", "app") }

// Payment 返回支付日志器。
func Payment() *slog.Logger { return root().With("scope", "payment") }

// Admin 返回管理员操作日志器。
func Admin() *slog.Logger { return root().With("scope", "admin") }

// Mail 返回邮件日志器。
func Mail() *slog.Logger { return root().With("scope", "mail") }

// FromContext 取出携带 request_id 的日志器。
func FromContext(ctx context.Context) *slog.Logger {
	l := L()
	if rid, ok := ctx.Value(RequestIDKey).(string); ok && rid != "" {
		l = l.With("request_id", rid)
	}
	return l
}

type ctxKey string

// RequestIDKey 是 context 中 request id 的键。
const RequestIDKey ctxKey = "request_id"

// Debug/Info/Warn/Error 便捷方法。
func Debug(msg string, args ...any) { root().Debug(msg, args...) }
func Info(msg string, args ...any)  { root().Info(msg, args...) }
func Warn(msg string, args ...any)  { root().Warn(msg, args...) }
func Error(msg string, args ...any) { root().Error(msg, args...) }
