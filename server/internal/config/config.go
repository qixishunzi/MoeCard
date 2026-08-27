// Package config 负责加载与校验应用配置。
//
// 加载优先级（后者覆盖前者）：
//  1. 内置默认值
//  2. config/config.yaml（可通过 -config 或 MOECARD_CONFIG 指定）
//  3. .env 文件
//  4. 进程环境变量
//
// 这样既满足"YAML 配置"也满足"12-factor 环境变量"，容器部署只用 env 即可。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

const (
	DriverSQLite = "sqlite"
	DriverMySQL  = "mysql"

	EnvProduction  = "production"
	EnvDevelopment = "development"
)

// Config 是应用的完整配置。
type Config struct {
	App      AppConfig      `yaml:"app"`
	Database DatabaseConfig `yaml:"database"`
	Log      LogConfig      `yaml:"log"`
	Storage  StorageConfig  `yaml:"storage"`
}

type AppConfig struct {
	Env  string `yaml:"env"`
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	Key  string `yaml:"key"` // 应用密钥，用于对称加密（预留）
	// DataKey 是静态加密主密钥：卡密内容与 TOTP 密钥落库前用它加密。
	// 留空表示不加密（向后兼容）。丢失该密钥将无法解出任何已加密的数据。
	DataKey     string        `yaml:"data_key"`
	JWTSecret   string        `yaml:"jwt_secret"`   // 管理员 JWT 签名密钥
	JWTExpire   time.Duration `yaml:"jwt_expire"`   // JWT 有效期
	FrontendURL string        `yaml:"frontend_url"` // 前端外部访问地址，用于拼接支付 return_url
	BaseURL     string        `yaml:"base_url"`     // 后端外部访问地址，用于拼接支付 notify_url
	TrustProxy  bool          `yaml:"trust_proxy"`  // 是否信任 X-Forwarded-For
}

type DatabaseConfig struct {
	Driver string       `yaml:"driver"`
	SQLite SQLiteConfig `yaml:"sqlite"`
	MySQL  MySQLConfig  `yaml:"mysql"`

	MaxOpenConns    int           `yaml:"max_open_conns"`
	MaxIdleConns    int           `yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
	SlowThreshold   time.Duration `yaml:"slow_threshold"`
}

type SQLiteConfig struct {
	Path string `yaml:"path"`
}

type MySQLConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
	Charset  string `yaml:"charset"`
	Params   string `yaml:"params"`
}

type LogConfig struct {
	Level  string `yaml:"level"`  // debug | info | warn | error
	Format string `yaml:"format"` // text | json
	File   string `yaml:"file"`   // 为空表示只输出到 stdout
}

type StorageConfig struct {
	Driver    string `yaml:"driver"`     // local
	LocalPath string `yaml:"local_path"` // 上传文件根目录
	URLPrefix string `yaml:"url_prefix"` // 对外访问前缀
	MaxSize   int64  `yaml:"max_size"`   // 单文件最大字节数
}

// Default 返回一份可直接运行的默认配置（SQLite 模式）。
func Default() *Config {
	return &Config{
		App: AppConfig{
			Env:        EnvDevelopment,
			Host:       "0.0.0.0",
			Port:       8080,
			JWTExpire:  12 * time.Hour,
			TrustProxy: false,
		},
		Database: DatabaseConfig{
			Driver:          DriverSQLite,
			SQLite:          SQLiteConfig{Path: "./data/moecard.db"},
			MySQL:           MySQLConfig{Host: "127.0.0.1", Port: 3306, Charset: "utf8mb4"},
			MaxOpenConns:    50,
			MaxIdleConns:    10,
			ConnMaxLifetime: time.Hour,
			SlowThreshold:   300 * time.Millisecond,
		},
		Log: LogConfig{Level: "info", Format: "text"},
		Storage: StorageConfig{
			Driver:    "local",
			LocalPath: "./storage/uploads",
			URLPrefix: "/uploads",
			MaxSize:   5 << 20, // 5MB
		},
	}
}

// Load 按优先级组装配置。configPath 为空时会尝试若干默认位置。
func Load(configPath string) (*Config, error) {
	cfg := Default()

	// .env 只在文件存在时加载；已存在的进程环境变量优先级更高（godotenv.Load 不覆盖）
	for _, p := range []string{".env", "../.env", "../../.env"} {
		if fileExists(p) {
			_ = godotenv.Load(p)
			break
		}
	}

	if configPath == "" {
		configPath = os.Getenv("MOECARD_CONFIG")
	}
	candidates := []string{configPath, "config/config.yaml", "../config/config.yaml"}
	for _, p := range candidates {
		if p == "" || !fileExists(p) {
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read config %s: %w", p, err)
		}
		if err := yaml.Unmarshal(raw, cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", p, err)
		}
		break
	}

	applyEnv(cfg)

	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	return cfg, cfg.Validate()
}

func applyEnv(c *Config) {
	envStr("APP_ENV", &c.App.Env)
	envStr("APP_HOST", &c.App.Host)
	envInt("APP_PORT", &c.App.Port)
	envStr("APP_KEY", &c.App.Key)
	envStr("JWT_SECRET", &c.App.JWTSecret)
	envStr("DATA_ENCRYPTION_KEY", &c.App.DataKey)
	envDuration("JWT_EXPIRE", &c.App.JWTExpire)
	envStr("FRONTEND_URL", &c.App.FrontendURL)
	envStr("BASE_URL", &c.App.BaseURL)
	envBool("TRUST_PROXY", &c.App.TrustProxy)

	envStr("DB_DRIVER", &c.Database.Driver)
	envStr("SQLITE_PATH", &c.Database.SQLite.Path)
	envStr("MYSQL_HOST", &c.Database.MySQL.Host)
	envInt("MYSQL_PORT", &c.Database.MySQL.Port)
	envStr("MYSQL_USERNAME", &c.Database.MySQL.Username)
	envStr("MYSQL_PASSWORD", &c.Database.MySQL.Password)
	envStr("MYSQL_DATABASE", &c.Database.MySQL.Database)
	envStr("MYSQL_CHARSET", &c.Database.MySQL.Charset)
	envStr("MYSQL_PARAMS", &c.Database.MySQL.Params)
	envInt("DB_MAX_OPEN_CONNS", &c.Database.MaxOpenConns)
	envInt("DB_MAX_IDLE_CONNS", &c.Database.MaxIdleConns)

	envStr("LOG_LEVEL", &c.Log.Level)
	envStr("LOG_FORMAT", &c.Log.Format)
	envStr("LOG_FILE", &c.Log.File)

	envStr("STORAGE_DRIVER", &c.Storage.Driver)
	envStr("STORAGE_LOCAL_PATH", &c.Storage.LocalPath)
	envStr("STORAGE_URL_PREFIX", &c.Storage.URLPrefix)
	envInt64("STORAGE_MAX_SIZE", &c.Storage.MaxSize)
}

func (c *Config) normalize() error {
	c.App.Env = strings.ToLower(strings.TrimSpace(c.App.Env))
	c.Database.Driver = strings.ToLower(strings.TrimSpace(c.Database.Driver))
	c.App.FrontendURL = strings.TrimRight(strings.TrimSpace(c.App.FrontendURL), "/")
	c.App.BaseURL = strings.TrimRight(strings.TrimSpace(c.App.BaseURL), "/")

	if c.App.JWTExpire <= 0 {
		c.App.JWTExpire = 12 * time.Hour
	}
	if c.Storage.MaxSize <= 0 {
		c.Storage.MaxSize = 5 << 20
	}
	if c.Database.Driver == DriverSQLite {
		abs, err := filepath.Abs(c.Database.SQLite.Path)
		if err != nil {
			return fmt.Errorf("resolve sqlite path: %w", err)
		}
		c.Database.SQLite.Path = abs
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return fmt.Errorf("create sqlite dir: %w", err)
		}
	}
	if c.Storage.Driver == "local" {
		abs, err := filepath.Abs(c.Storage.LocalPath)
		if err != nil {
			return fmt.Errorf("resolve storage path: %w", err)
		}
		c.Storage.LocalPath = abs
	}
	return nil
}

// Validate 在启动阶段就拦住会导致线上事故的配置错误。
func (c *Config) Validate() error {
	var errs []string

	if c.App.Port <= 0 || c.App.Port > 65535 {
		errs = append(errs, "app.port 必须在 1-65535 之间")
	}
	switch c.Database.Driver {
	case DriverSQLite:
		if c.Database.SQLite.Path == "" {
			errs = append(errs, "sqlite.path 不能为空")
		}
	case DriverMySQL:
		m := c.Database.MySQL
		if m.Host == "" || m.Database == "" || m.Username == "" {
			errs = append(errs, "mysql 需要配置 host / username / database")
		}
	default:
		errs = append(errs, "database.driver 必须是 sqlite 或 mysql")
	}

	if c.IsProduction() {
		// 生产环境绝不允许使用空/弱密钥 —— 空 JWT 密钥意味着任何人都能伪造管理员 token。
		if len(c.App.JWTSecret) < 32 {
			errs = append(errs, "生产环境 JWT_SECRET 长度必须 >= 32（可用 openssl rand -hex 32 生成）")
		}
		if c.App.BaseURL == "" {
			errs = append(errs, "生产环境必须配置 BASE_URL（用于生成支付异步通知地址）")
		}
	} else if c.App.JWTSecret == "" {
		// 开发环境自动生成，避免每次重启都要配置；但会在日志里提示。
		c.App.JWTSecret = "moecard-dev-insecure-secret-do-not-use-in-production"
	}

	if len(errs) > 0 {
		return errors.New("配置校验失败:\n  - " + strings.Join(errs, "\n  - "))
	}
	return nil
}

func (c *Config) IsProduction() bool { return c.App.Env == EnvProduction }

func (c *Config) Addr() string { return fmt.Sprintf("%s:%d", c.App.Host, c.App.Port) }

// DSN 返回当前驱动的连接串。
func (c *Config) DSN() string {
	if c.Database.Driver == DriverMySQL {
		m := c.Database.MySQL
		charset := m.Charset
		if charset == "" {
			charset = "utf8mb4"
		}
		params := m.Params
		if params == "" {
			// parseTime 必须开启，否则 DATETIME 无法扫描进 time.Time；loc=UTC 保证时间一致性。
			params = "parseTime=True&loc=UTC&timeout=10s&readTimeout=30s&writeTimeout=30s"
		}
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&%s",
			m.Username, m.Password, m.Host, m.Port, m.Database, charset, params)
	}
	// _pragma 通过 DSN 传给 modernc 驱动：
	//   busy_timeout  等待写锁而非立刻返回 SQLITE_BUSY
	//   journal_mode=WAL  读写并发
	//   foreign_keys  开启外键约束
	//   synchronous=NORMAL  WAL 下的安全/性能平衡点
	return c.Database.SQLite.Path +
		"?_pragma=busy_timeout(10000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(1)"
}

// ---- env helpers ----

func envStr(key string, dst *string) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		*dst = v
	}
}

func envInt(key string, dst *int) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*dst = n
		}
	}
}

func envInt64(key string, dst *int64) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			*dst = n
		}
	}
}

func envBool(key string, dst *bool) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			*dst = b
		}
	}
}

func envDuration(key string, dst *time.Duration) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			*dst = d
		}
	}
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
