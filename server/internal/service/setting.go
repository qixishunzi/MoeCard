// Package service 是业务逻辑层。
//
// 规则：
//   - 事务边界只在这一层（通过 db.Tx）
//   - 禁止出现 gin.Context / HTTP 相关类型
//   - 对外返回 *api.Error，让 handler 无需判断具体错误类型
package service

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/database"
	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/internal/mail"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/utils"
)

// SettingService 管理系统配置，并在内存中缓存。
//
// 配置几乎每个请求都要读（商城名、是否允许下单、时区…），
// 每次查库既慢又没必要。这里用读写锁保护的内存快照，
// 写入时整体刷新，保证一致性。
type SettingService struct {
	repo *repository.SettingRepo
	db   *database.DB

	mu    sync.RWMutex
	cache map[string]string
}

// NewSettingService 构造并加载缓存。
func NewSettingService(db *database.DB, repo *repository.SettingRepo) *SettingService {
	s := &SettingService{repo: repo, db: db, cache: map[string]string{}}
	if err := s.Reload(context.Background()); err != nil {
		logger.L().Warn("加载系统配置失败，将使用默认值", "err", err)
	}
	return s
}

// Reload 从数据库重新加载配置。
func (s *SettingService) Reload(ctx context.Context) error {
	kv, err := s.repo.All(ctx, nil)
	if err != nil {
		return err
	}
	merged := model.DefaultSettings()
	for k, v := range kv {
		merged[k] = v
	}
	s.mu.Lock()
	s.cache = merged
	s.mu.Unlock()
	return nil
}

// EnsureDefaults 把缺失的默认配置写入数据库（首次启动时调用）。
func (s *SettingService) EnsureDefaults(ctx context.Context) error {
	existing, err := s.repo.All(ctx, nil)
	if err != nil {
		return err
	}
	missing := map[string]string{}
	for k, v := range model.DefaultSettings() {
		if _, ok := existing[k]; !ok {
			missing[k] = v
		}
	}
	if len(missing) > 0 {
		if err := s.db.Tx(ctx, func(tx *gorm.DB) error {
			return s.repo.SetMany(ctx, tx, missing)
		}); err != nil {
			return err
		}
		logger.L().Info("已写入默认系统配置", "count", len(missing))
	}
	return s.Reload(ctx)
}

// Get 读取配置项。
func (s *SettingService) Get(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cache[key]
}

// GetInt 读取整数配置项。
func (s *SettingService) GetInt(key string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(s.Get(key)))
	if err != nil {
		return def
	}
	return v
}

// GetBool 读取布尔配置项（"1" / "true" 为真）。
func (s *SettingService) GetBool(key string) bool {
	v := strings.ToLower(strings.TrimSpace(s.Get(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// All 返回配置快照（含敏感项原值，仅供内部使用）。
func (s *SettingService) All() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.cache))
	for k, v := range s.cache {
		out[k] = v
	}
	return out
}

// AllMasked 返回脱敏后的配置快照（用于后台接口出参）。
func (s *SettingService) AllMasked() map[string]string {
	out := s.All()
	for k := range out {
		if model.IsSecretSettingKey(k) {
			out[k] = utils.MaskSecret(out[k])
		}
	}
	return out
}

// Update 批量更新配置。
//
// 敏感项的"未修改"语义在这里落地：如果提交上来的值是掩码形态，
// 就保留数据库中的旧值。否则用户改一个无关配置就会把 SMTP 密码
// 覆盖成一串星号 —— 这是典型的线上事故。
func (s *SettingService) Update(ctx context.Context, kv map[string]string) error {
	current := s.All()
	final := make(map[string]string, len(kv))

	for k, v := range kv {
		if model.IsSecretSettingKey(k) && utils.IsSecretUnchanged(v) {
			continue // 保留旧值：直接不写这一项
		}
		// 入口路径统一规范化后再存，免得库里躺着 "/Admin/" 这种值，
		// 之后每处比较都得先想一遍要不要去斜杠、要不要转小写
		if k == model.SetAdminPath {
			v = model.NormalizeAdminPath(v)
		}
		final[k] = v
	}

	if err := validateSettings(final, current); err != nil {
		return err
	}
	if len(final) == 0 {
		return nil
	}

	if err := s.db.Tx(ctx, func(tx *gorm.DB) error {
		return s.repo.SetMany(ctx, tx, final)
	}); err != nil {
		return api.WrapError(api.CodeInternal, err)
	}
	return s.Reload(ctx)
}

// Set 更新单项配置。
func (s *SettingService) Set(ctx context.Context, key, value string) error {
	return s.Update(ctx, map[string]string{key: value})
}

// validateSettings 校验配置值合法性，把错误挡在写库之前。
func validateSettings(kv, current map[string]string) error {
	for k, v := range kv {
		switch k {
		case model.SetOrderExpireMinutes:
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 1 || n > 1440 {
				return api.NewErrorf(api.CodeSettingInvalid, "订单超时时间必须是 1-1440 之间的整数（分钟）")
			}
		case model.SetSMTPPort:
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 1 || n > 65535 {
				return api.NewErrorf(api.CodeSettingInvalid, "SMTP 端口不合法")
			}
		case model.SetSMTPFromEmail:
			if strings.TrimSpace(v) != "" {
				if err := utils.ValidateEmail(strings.TrimSpace(v)); err != nil {
					return api.NewErrorf(api.CodeSettingInvalid, "发件人邮箱不合法: %s", err.Error())
				}
			}
		case model.SetSMTPEncrypt:
			switch strings.ToLower(strings.TrimSpace(v)) {
			case mail.EncryptionNone, mail.EncryptionSSL, mail.EncryptionSTARTTLS:
			default:
				return api.NewErrorf(api.CodeSettingInvalid, "SMTP 加密方式必须是 none / ssl / starttls")
			}
		case model.SetTimezone:
			if strings.TrimSpace(v) != "" {
				if _, err := time.LoadLocation(strings.TrimSpace(v)); err != nil {
					return api.NewErrorf(api.CodeSettingInvalid, "时区名称不合法: %s", v)
				}
			}
		case model.SetSiteName:
			if strings.TrimSpace(v) == "" {
				return api.NewErrorf(api.CodeSettingInvalid, "商城名称不能为空")
			}
		case model.SetNoticeForceSecond:
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 1 || n > 60 {
				return api.NewErrorf(api.CodeSettingInvalid,
					"强制阅读时长必须是 1-60 之间的整数（秒）")
			}
		case model.SetClientIPHeaders:
			if err := model.ValidateIPHeaders(v); err != nil {
				return api.NewErrorf(api.CodeSettingInvalid, "%s", err.Error())
			}
		case model.SetBanners:
			if err := model.ValidateBanners(v); err != nil {
				return api.NewErrorf(api.CodeSettingInvalid, "%s", err.Error())
			}
		case model.SetContacts:
			if err := model.ValidateContacts(v); err != nil {
				return api.NewErrorf(api.CodeSettingInvalid, "%s", err.Error())
			}
		case model.SetAdminPath:
			if err := model.ValidateAdminPath(v); err != nil {
				return api.NewErrorf(api.CodeSettingInvalid, "%s", err.Error())
			}
		case model.SetTurnstileWidgetSize:
			switch strings.TrimSpace(v) {
			case "normal", "flexible", "compact":
			default:
				return api.NewErrorf(api.CodeSettingInvalid,
					"验证控件尺寸必须是 normal / flexible / compact")
			}
		}
	}

	// 开了人机验证却没填密钥，会把所有人挡在门外 —— 在写库之前就拦住。
	// 用 merged 判断而不是只看本次提交：站长可能这次只翻了开关，
	// 密钥是上一次存进去的。
	if turnstileOn(kv, current) {
		if settingValue(kv, current, model.SetTurnstileSiteKey) == "" {
			return api.NewErrorf(api.CodeSettingInvalid,
				"开启人机验证前请先填写站点密钥（Site Key）")
		}
		// 密钥可能是脱敏值被上层跳过了，所以看已存的那份
		if strings.TrimSpace(current[model.SetTurnstileSecretKey]) == "" &&
			strings.TrimSpace(kv[model.SetTurnstileSecretKey]) == "" {
			return api.NewErrorf(api.CodeSettingInvalid,
				"开启人机验证前请先填写密钥（Secret Key）")
		}
	}
	return nil
}

// settingValue 取「本次提交的值，没提交就取已存的值」。
func settingValue(kv, current map[string]string, key string) string {
	if v, ok := kv[key]; ok {
		return strings.TrimSpace(v)
	}
	return strings.TrimSpace(current[key])
}

// turnstileOn 判断这次保存之后总开关是不是打开状态。
func turnstileOn(kv, current map[string]string) bool {
	v := strings.ToLower(settingValue(kv, current, model.SetTurnstileEnabled))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// ---- 常用配置的语义化访问器 ----

// SiteName 返回商城名称。
func (s *SettingService) SiteName() string {
	if v := s.Get(model.SetSiteName); v != "" {
		return v
	}
	return "MoeCard"
}

// Timezone 返回商城时区。
func (s *SettingService) Timezone() string {
	if v := s.Get(model.SetTimezone); v != "" {
		return v
	}
	return "Asia/Shanghai"
}

// CurrencySymbol 返回货币符号。
func (s *SettingService) CurrencySymbol() string {
	if v := s.Get(model.SetCurrencySymbol); v != "" {
		return v
	}
	return "¥"
}

// OrderExpireDuration 返回订单超时时长。
func (s *SettingService) OrderExpireDuration() time.Duration {
	m := s.GetInt(model.SetOrderExpireMinutes, 15)
	if m < 1 {
		m = 15
	}
	return time.Duration(m) * time.Minute
}

// AllowOrder 是否允许下单。
func (s *SettingService) AllowOrder() bool { return s.GetBool(model.SetAllowOrder) }

// CustomIPHeaders 站长配置的自定义客户端 IP 请求头。
//
// 名字对齐 middleware.ClientIPConfig 接口 —— SettingService 直接当它的实现用，
// 不用再包一层适配器。
func (s *SettingService) CustomIPHeaders() []string {
	return model.ParseIPHeaders(s.Get(model.SetClientIPHeaders))
}

// ShowSales 前台是否显示商品的已售数量。
func (s *SettingService) ShowSales() bool { return s.GetBool(model.SetShowSales) }

// MaintenanceMode 是否处于维护模式。
func (s *SettingService) MaintenanceMode() bool { return s.GetBool(model.SetMaintenanceMode) }

// IsInstalled 系统是否已完成初始化。
func (s *SettingService) IsInstalled() bool { return s.GetBool(model.SetInstalled) }

// MailConfig 组装 SMTP 配置。
func (s *SettingService) MailConfig() mail.Config {
	return mail.Config{
		Host:       s.Get(model.SetSMTPHost),
		Port:       s.GetInt(model.SetSMTPPort, 465),
		Username:   s.Get(model.SetSMTPUsername),
		Password:   s.Get(model.SetSMTPPassword),
		FromEmail:  s.Get(model.SetSMTPFromEmail),
		FromName:   s.Get(model.SetSMTPFromName),
		Encryption: s.Get(model.SetSMTPEncrypt),
	}
}

// MailEnabled 是否启用邮件通知。
func (s *SettingService) MailEnabled() bool {
	return s.GetBool(model.SetSMTPEnabled) && s.MailConfig().Validate() == nil
}

// PingDB 探活数据库，供 /health 使用。
func (s *SettingService) PingDB(ctx context.Context) error {
	sqlDB, err := s.db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// Contacts 返回已配置的客服联系方式（含算好的链接）。
//
// 只认 contacts 这一个来源。老版本那个单值 contact 已经由迁移 0006
// 搬进这里并删除了 —— 留着一个后台改不到、却还在给前台兜底的字段，
// 只会让站长以为自己删干净了。
func (s *SettingService) Contacts() []model.Contact {
	return model.ParseContacts(s.Get(model.SetContacts))
}

// AdminPath 返回后台入口路径（不含斜杠），异常值一律回落到默认值。
//
// 兜底很重要：这个值一旦坏掉，后台就再也进不去了。
func (s *SettingService) AdminPath() string {
	v := model.NormalizeAdminPath(s.Get(model.SetAdminPath))
	if v == "" || model.ValidateAdminPath(v) != nil {
		return model.DefaultAdminPath
	}
	return v
}

// ---- Turnstile 人机验证 ----
//
// 这几个方法同时实现了 middleware.TurnstileConfig 接口。
// 中间件只认接口不认具体类型，避免 middleware 反向依赖 service 形成环。

// Enabled 返回人机验证总开关。
func (s *SettingService) Enabled() bool {
	return s.GetBool(model.SetTurnstileEnabled)
}

// SceneOn 返回某个场景是否需要人机验证。
func (s *SettingService) SceneOn(scene model.TurnstileScene) bool {
	key := model.TurnstileSceneKey(scene)
	if key == "" {
		return false
	}
	return s.GetBool(key)
}

// Secret 返回 Turnstile 密钥（服务端校验用，绝不出现在任何出参里）。
func (s *SettingService) Secret() string {
	return strings.TrimSpace(s.Get(model.SetTurnstileSecretKey))
}

// TurnstileSiteKey 返回站点密钥。
// 它本来就要写进页面 HTML，属于公开信息，可以给前台。
func (s *SettingService) TurnstileSiteKey() string {
	return strings.TrimSpace(s.Get(model.SetTurnstileSiteKey))
}

// TurnstileReady 判断配置是否完整可用。
// 开了开关但两个密钥没填全时返回 false，供后台提示用。
func (s *SettingService) TurnstileReady() bool {
	return s.Enabled() && s.TurnstileSiteKey() != "" && s.Secret() != ""
}

// InstalledAt 返回系统部署时间（首次启动写入默认配置的时刻）。
func (s *SettingService) InstalledAt(ctx context.Context) (time.Time, error) {
	return s.repo.FirstSeenAt(ctx, nil)
}
