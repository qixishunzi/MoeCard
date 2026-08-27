package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// SystemSetting 是 key/value 形式的系统配置。
//
// IsSecret = true 的项在任何出参中都必须脱敏；
// 提交时若值等于掩码常量，则保留数据库中的旧值（见 §57 要求）。
type SystemSetting struct {
	ID        uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Key       string    `gorm:"column:setting_key;size:100;uniqueIndex" json:"key"`
	Value     string    `gorm:"column:value" json:"value"`
	IsSecret  bool      `gorm:"column:is_secret" json:"is_secret"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (SystemSetting) TableName() string { return "system_settings" }

// 系统配置键名。集中定义，避免各处硬编码字符串导致拼写错误。
const (
	// 商城基础
	SetSiteName        = "site_name"
	SetSiteLogo        = "site_logo"
	SetSiteTitle       = "site_title"
	SetSiteDescription = "site_description"
	SetSiteKeywords    = "site_keywords"
	SetSiteNotice      = "site_notice"
	SetSiteFooter      = "site_footer"
	SetICP             = "icp"
	SetCurrency        = "currency"        // CNY / USD ...
	SetCurrencySymbol  = "currency_symbol" // ¥ / $
	SetTimezone        = "timezone"        // Asia/Shanghai

	// 开关
	SetAllowOrder      = "allow_order"
	SetMaintenanceMode = "maintenance_mode"
	SetMaintenanceText = "maintenance_text"
	// 前台要不要显示"已售 N"。销量是商家的经营数据，
	// 新店铺的个位数销量摆在商品卡上反而劝退，所以给个开关。
	SetShowSales = "show_sales"
	// 自定义客户端 IP 请求头。多个用逗号或换行分隔，解析时优先于内置列表。
	SetClientIPHeaders = "client_ip_headers"

	// 订单
	SetOrderExpireMinutes = "order_expire_minutes"

	// SMTP
	SetSMTPHost      = "smtp_host"
	SetSMTPPort      = "smtp_port"
	SetSMTPUsername  = "smtp_username"
	SetSMTPPassword  = "smtp_password" // secret
	SetSMTPFromEmail = "smtp_from_email"
	SetSMTPFromName  = "smtp_from_name"
	SetSMTPEncrypt   = "smtp_encryption" // none | ssl | starttls
	SetSMTPEnabled   = "smtp_enabled"

	// 邮件模板
	SetMailPaidSubject      = "mail_paid_subject"
	SetMailPaidBody         = "mail_paid_body"
	SetMailDeliverSubject   = "mail_deliver_subject"
	SetMailDeliverBody      = "mail_deliver_body"
	SetMailManualSubject    = "mail_manual_subject"
	SetMailManualBody       = "mail_manual_body"
	SetMailNotifyOnPaid     = "mail_notify_on_paid"
	SetMailNotifyOnDelivery = "mail_notify_on_delivery"

	// 商家即时通知
	SetNotifyEnabled  = "notify_enabled"  // 总开关
	SetNotifyChannels = "notify_channels" // 已启用的渠道，逗号分隔：telegram,bark,...
	// 各渠道配置以 notify_cfg_<channel>_<field> 的形式存储，
	// 由 NotifyConfigKey 拼装 —— 新增渠道无需再改这里。
	SetNotifyOnPaid      = "notify_on_paid"
	SetNotifyOnManual    = "notify_on_manual"
	SetNotifyOnAttention = "notify_on_attention"
	SetNotifyOnLowStock  = "notify_on_low_stock"
	SetNotifyOnRefund    = "notify_on_refund"

	// 库存告警
	SetLowStockThreshold = "low_stock_threshold" // 全局默认阈值，商品可单独覆盖

	// 人机验证（Cloudflare Turnstile）
	//
	// 站点密钥是公开的（本来就要写进页面 HTML），密钥必须脱敏。
	// 每个场景一个开关：验证码会挡在正常用户面前，开在哪里应该由店主自己决定，
	// 而不是一刀切全开。
	SetTurnstileEnabled    = "turnstile_enabled"
	SetTurnstileSiteKey    = "turnstile_site_key"
	SetTurnstileSecretKey  = "turnstile_secret_key"
	SetTurnstileOnLogin    = "turnstile_on_admin_login" // 后台登录
	SetTurnstileOnOrder    = "turnstile_on_order"       // 下单
	SetTurnstileOnQuery    = "turnstile_on_order_query" // 订单查询
	SetTurnstileOnCoupon   = "turnstile_on_coupon"      // 优惠券校验
	SetTurnstileWidgetSize = "turnstile_widget_size"    // normal | flexible | compact

	// 后台入口路径。默认 admin，可改成自定义的一串，减少被扫描器撞见的机会。
	SetAdminPath = "admin_path"

	// 客服联系方式（JSON 数组）。旧的单值 contact 仍保留，
	// 老站点升级上来时会被自动并进来，不用手工重填。
	SetContacts = "contacts"

	// 首页顶部轮播图（JSON 数组）。取代了原来那块写死的色块横幅 ——
	// 店主想放什么就放什么，还能把图和商品绑在一起。
	SetBanners = "banners"

	// 公告弹窗。
	//
	// 公告写在页面里很容易被划过去；做成弹窗才真的会被看见。
	// 强制阅读是给"必读条款"用的：倒计时结束前关不掉。
	SetNoticePopup       = "notice_popup_enabled"
	SetNoticeForce       = "notice_force_read"
	SetNoticeForceSecond = "notice_force_seconds"

	// 初始化标记
	SetInstalled = "installed"
)

// DefaultAdminPath 是出厂的后台入口。
const DefaultAdminPath = "admin"

// reservedPaths 是不能被后台入口占用的一级路径。
//
// 这份名单必须跟着实际路由走 —— 新增一级路由时要同步加进来，
// 否则店主可以把后台入口设成 /product，然后整个商品详情页就再也打不开了。
var reservedPaths = map[string]bool{
	// 服务端路由
	"api": true, "uploads": true, "health": true, "assets": true,
	// 前端一级路由
	"setup": true, "category": true, "product": true, "checkout": true,
	"pay": true, "order": true, "orders": true,
	// 静态文件与常见约定
	"favicon.svg": true, "favicon.ico": true, "robots.txt": true,
	"index.html": true, "sitemap.xml": true, ".well-known": true,
	"static": true, "public": true, "docs": true,
}

// Banner 是首页顶部轮播图的一张。
type Banner struct {
	// Image 是图片地址（后台上传得到的相对路径，或外链）
	Image string `json:"image"`
	// Title 用作图片的替代文字。没有它，读屏软件只会念"图片"
	Title string `json:"title,omitempty"`
	// ProductID 为 0 表示这张图不跳转
	ProductID uint64 `json:"product_id,omitempty"`
}

// maxBanners 限制轮播图数量。
//
// 轮播本来就是个转得越多、被看到的越少的东西；放十几张只会让
// 第一张之后的全部沦为背景。
const maxBanners = 8

// maxIPHeaders 限制自定义头部数量。
//
// 真实场景里最多也就"CDN 一个 + 内网反代一个"，多到十几个只说明配错了，
// 而每多一个都是每个请求都要多查一次的开销。
const maxIPHeaders = 5

// headerNamePattern 是 RFC 7230 的 token 字符集。
//
// 头部名里混进空格或冒号的话，取到的永远是空值 —— 与其让站长对着
// 一个"配了但不生效"的框排查半天，不如保存时就说清楚。
var headerNamePattern = regexp.MustCompile(`^[A-Za-z0-9!#$%&\'*+\-.^_` + "`" + `|~]+$`)

// ParseIPHeaders 把配置里的自定义头部拆成列表。逗号和换行都认。
func ParseIPHeaders(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ';'
	})
	out := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, f := range fields {
		name := strings.TrimSpace(f)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}

// ValidateIPHeaders 校验后台提交的自定义头部。
func ValidateIPHeaders(raw string) error {
	list := ParseIPHeaders(raw)
	if len(list) > maxIPHeaders {
		return fmt.Errorf("自定义 IP 请求头最多 %d 个", maxIPHeaders)
	}
	for _, name := range list {
		if len(name) > 64 {
			return fmt.Errorf("请求头名称过长: %s", name)
		}
		if !headerNamePattern.MatchString(name) {
			return fmt.Errorf("请求头名称不合法: %s（只能是字母、数字和 - _ 等符号，不能有空格或冒号）", name)
		}
	}
	return nil
}

// ParseBanners 解析已保存的轮播图。坏数据返回空列表，不让首页打不开。
func ParseBanners(raw string) []Banner {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var list []Banner
	if json.Unmarshal([]byte(raw), &list) != nil {
		return nil
	}
	out := make([]Banner, 0, len(list))
	for _, b := range list {
		b.Image = strings.TrimSpace(b.Image)
		if b.Image == "" {
			continue
		}
		b.Title = strings.TrimSpace(b.Title)
		out = append(out, b)
	}
	return out
}

// ValidateBanners 校验后台提交的轮播图 JSON。
func ValidateBanners(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var list []Banner
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return errors.New("轮播图格式不正确")
	}
	if len(list) > maxBanners {
		return fmt.Errorf("轮播图最多 %d 张", maxBanners)
	}
	for i, b := range list {
		img := strings.TrimSpace(b.Image)
		if img == "" {
			return fmt.Errorf("第 %d 张还没有选图片", i+1)
		}
		if len(img) > 500 {
			return errors.New("图片地址过长")
		}
		// 只接受站内相对路径或 http(s) 外链。
		// 放开 javascript: 之类的伪协议会直接变成一个点击即执行的 XSS。
		if !strings.HasPrefix(img, "/") &&
			!strings.HasPrefix(img, "http://") && !strings.HasPrefix(img, "https://") {
			return fmt.Errorf("第 %d 张的图片地址必须是站内路径或 http(s) 链接", i+1)
		}
		if len([]rune(b.Title)) > 60 {
			return errors.New("轮播图说明最长 60 字")
		}
	}
	return nil
}

// ContactType 是一种客服联系方式。
type ContactType string

const (
	ContactTelegram ContactType = "telegram"
	ContactWhatsApp ContactType = "whatsapp"
	ContactWeChat   ContactType = "wechat"
	ContactQQ       ContactType = "qq"
	ContactEmail    ContactType = "email"
)

// ContactMeta 描述一种联系方式怎么展示、点了之后干什么。
type ContactMeta struct {
	Type ContactType `json:"type"`
	// Label 是默认显示名，管理员可以覆盖
	Label string `json:"label"`
	// Action 决定点击行为：link 直接跳转，copy 复制到剪贴板。
	//
	// 为什么不统一成跳转：微信号、QQ 号、邮箱这类，用户要的是把它
	// 粘到自己的聊天软件里，跳转反而帮倒忙（微信压根没有 web 协议）。
	Action string `json:"action"` // link | copy
	// Placeholder 给后台输入框做示例
	Placeholder string `json:"placeholder"`
}

// ContactTypes 是全部可选的联系方式，顺序即后台下拉与前台展示的顺序。
var ContactTypes = []ContactMeta{
	{ContactTelegram, "Telegram", "link", "@yourname 或 https://t.me/yourname"},
	{ContactWhatsApp, "WhatsApp", "link", "8613800138000 或 https://wa.me/8613800138000"},
	{ContactWeChat, "微信", "copy", "微信号"},
	{ContactQQ, "QQ", "copy", "QQ 号"},
	{ContactEmail, "邮箱", "copy", "support@example.com"},
}

// ContactMetaOf 按类型取元信息，未知类型返回 nil。
func ContactMetaOf(t ContactType) *ContactMeta {
	for i := range ContactTypes {
		if ContactTypes[i].Type == t {
			return &ContactTypes[i]
		}
	}
	return nil
}

// Contact 是一条已配置的联系方式。
type Contact struct {
	Type  ContactType `json:"type"`
	Value string      `json:"value"`
	// Label 为空时前台用类型的默认名
	Label string `json:"label,omitempty"`
	// URL 由服务端算好给前台，前端不必知道各家的链接规则
	URL string `json:"url,omitempty"`
	// Action 同 ContactMeta.Action
	Action string `json:"action,omitempty"`
}

// maxContacts 限制条数：联系方式是给人看的，堆几十条只会让人找不到重点。
const maxContacts = 10

// ParseContacts 解析已保存的联系方式，顺带补上展示用的 URL 与动作。
//
// 解析失败返回空列表而不是报错：一条坏配置不该让整个前台打不开。
func ParseContacts(raw string) []Contact {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var list []Contact
	if json.Unmarshal([]byte(raw), &list) != nil {
		return nil
	}
	out := make([]Contact, 0, len(list))
	for _, c := range list {
		meta := ContactMetaOf(c.Type)
		if meta == nil || strings.TrimSpace(c.Value) == "" {
			continue
		}
		c.Value = strings.TrimSpace(c.Value)
		if c.Label == "" {
			c.Label = meta.Label
		}
		c.Action = meta.Action
		c.URL = contactURL(c.Type, c.Value)
		out = append(out, c)
	}
	return out
}

// contactURL 把用户填的内容拼成可点击的链接。
//
// 只有 Telegram / WhatsApp 有稳定的 web 跳转协议，其余留空由前台走复制。
func contactURL(t ContactType, v string) string {
	if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
		return v // 已经是完整链接，原样用
	}
	switch t {
	case ContactTelegram:
		return "https://t.me/" + strings.TrimPrefix(v, "@")
	case ContactWhatsApp:
		// wa.me 只认纯数字国际区号格式，把常见的 + - 空格去掉
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, v)
		if digits == "" {
			return ""
		}
		return "https://wa.me/" + digits
	}
	return ""
}

// ValidateContacts 校验后台提交的联系方式 JSON。
func ValidateContacts(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var list []Contact
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return errors.New("联系方式格式不正确")
	}
	if len(list) > maxContacts {
		return fmt.Errorf("联系方式最多 %d 条", maxContacts)
	}
	seen := map[string]bool{}
	for _, c := range list {
		if ContactMetaOf(c.Type) == nil {
			return fmt.Errorf("不支持的联系方式类型：%s", c.Type)
		}
		v := strings.TrimSpace(c.Value)
		if v == "" {
			return fmt.Errorf("「%s」的内容不能为空", c.Type)
		}
		if len([]rune(v)) > 200 {
			return errors.New("联系方式内容过长")
		}
		if len([]rune(c.Label)) > 20 {
			return errors.New("联系方式名称最长 20 字")
		}
		// 同一类型可以有多个（比如两个客服微信），但完全相同的重复没意义
		key := string(c.Type) + "|" + v
		if seen[key] {
			return fmt.Errorf("「%s」里有重复的内容", c.Type)
		}
		seen[key] = true
		if c.Type == ContactEmail && !strings.Contains(v, "@") {
			return errors.New("邮箱格式不正确")
		}
	}
	return nil
}

// NormalizeAdminPath 规范化后台入口：去空白、去两端斜杠、转小写。
func NormalizeAdminPath(v string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(v), "/"))
}

// adminPathPattern 限定可用字符：小写字母、数字、中划线、下划线，
// 且首尾必须是字母或数字（避免 "-abc-" 这种在 URL 里怪异的形态）。
var adminPathPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*[a-z0-9]$`)

// ValidateAdminPath 校验后台入口路径。
//
// 保留默认值 admin 是为了兼容已经在跑的站点 —— 强制它们改路径
// 会让所有人的收藏夹和文档一起失效。但只要动手改，就必须够长：
// 一个 4 位的自定义路径几分钟就能被穷举出来，还不如不改。
func ValidateAdminPath(v string) error {
	v = NormalizeAdminPath(v)
	if v == "" {
		return errors.New("后台入口不能为空")
	}
	if v == DefaultAdminPath {
		return nil // 出厂值，放行
	}
	if len([]rune(v)) < 8 {
		return errors.New("自定义后台入口至少 8 位（太短等于没改，几分钟就能被扫出来）")
	}
	if len(v) > 32 {
		return errors.New("后台入口最长 32 位")
	}
	if !adminPathPattern.MatchString(v) {
		return errors.New("后台入口只能包含小写字母、数字、中划线和下划线，且首尾必须是字母或数字")
	}
	if reservedPaths[v] {
		return errors.New("这个路径已被商城自身占用，请换一个")
	}
	return nil
}

// TurnstileScene 是可以单独开关人机验证的场景。
type TurnstileScene string

const (
	TurnstileSceneLogin  TurnstileScene = "admin_login"
	TurnstileSceneOrder  TurnstileScene = "order"
	TurnstileSceneQuery  TurnstileScene = "order_query"
	TurnstileSceneCoupon TurnstileScene = "coupon"
)

// TurnstileSceneKey 返回某个场景对应的开关配置项键名。
func TurnstileSceneKey(scene TurnstileScene) string {
	switch scene {
	case TurnstileSceneLogin:
		return SetTurnstileOnLogin
	case TurnstileSceneOrder:
		return SetTurnstileOnOrder
	case TurnstileSceneQuery:
		return SetTurnstileOnQuery
	case TurnstileSceneCoupon:
		return SetTurnstileOnCoupon
	}
	return ""
}

// NotifyConfigKey 拼装某个通知渠道某个字段的设置项键名。
func NotifyConfigKey(channel, field string) string {
	return "notify_cfg_" + channel + "_" + field
}

// SecretSettingKeys 列出所有需要脱敏的配置项。
//
// 通知渠道的密钥类字段不在这里静态列举 —— 它们由 notify 包的
// ConfigField.Secret 决定，在 setting 服务里动态判定（见 IsSecretSettingKey）。
var SecretSettingKeys = map[string]bool{
	SetSMTPPassword:       true,
	SetTurnstileSecretKey: true,
}

// dynamicSecretKeys / dynamicKnownKeys 由启动时注册进来。
//
// 通知渠道的配置项键名是 notify_cfg_<渠道>_<字段> 动态拼出来的，
// 没法写进 DefaultSettings 的静态表 —— 但后台保存设置时会拿白名单校验，
// 不登记就会被当成"未知配置项"整个请求打回。
var (
	dynamicSecretKeys = map[string]bool{}
	dynamicKnownKeys  = map[string]bool{}
)

// RegisterSecretSettingKey 把一个配置项标记为敏感（出参脱敏、空值保留旧值）。
func RegisterSecretSettingKey(key string) {
	dynamicSecretKeys[key] = true
	dynamicKnownKeys[key] = true
}

// RegisterKnownSettingKey 把一个配置项登记为合法键名。
func RegisterKnownSettingKey(key string) { dynamicKnownKeys[key] = true }

// IsKnownSettingKey 判断配置项是否是系统认识的键。
func IsKnownSettingKey(key string) bool {
	if _, ok := defaultSettings[key]; ok {
		return true
	}
	return dynamicKnownKeys[key]
}

// IsSecretSettingKey 判断配置项是否敏感。
func IsSecretSettingKey(key string) bool {
	return SecretSettingKeys[key] || dynamicSecretKeys[key]
}

// defaultSettings 是默认配置的静态表。
var defaultSettings = map[string]string{
	SetSiteName: "MoeCard",
	// 留空：浏览器标题回落到商城名称。
	// 给它一个写死的默认值会让店主改完商城名后，标签页仍显示出厂宣传语。
	SetSiteTitle:       "",
	SetSiteDescription: "轻量、安全、开箱即用的数字商品商城",
	SetSiteKeywords:    "卡密,自动发货,数字商品",
	SetSiteLogo:        "",
	SetSiteNotice:      "欢迎光临！本站商品支持自动发货，付款后即可在订单详情查看卡密。",
	SetSiteFooter:      "",
	SetICP:             "",
	SetCurrency:        "CNY",
	SetCurrencySymbol:  "¥",
	SetTimezone:        "Asia/Shanghai",

	SetAllowOrder:      "1",
	SetShowSales:       "1",
	SetClientIPHeaders: "",
	SetMaintenanceMode: "0",
	SetMaintenanceText: "商城正在维护升级，请稍后再来。",

	SetOrderExpireMinutes: "15",

	SetSMTPHost:      "",
	SetSMTPPort:      "465",
	SetSMTPUsername:  "",
	SetSMTPPassword:  "",
	SetSMTPFromEmail: "",
	SetSMTPFromName:  "MoeCard",
	SetSMTPEncrypt:   "ssl",
	SetSMTPEnabled:   "0",

	SetNotifyEnabled:     "0",
	SetNotifyChannels:    "",
	SetNotifyOnPaid:      "0", // 单量大时每单都推会很吵，默认关
	SetNotifyOnManual:    "1", // 手动发货必须立刻知道
	SetNotifyOnAttention: "1", // 收了钱发不出货，必须立刻知道
	SetNotifyOnLowStock:  "1",
	SetNotifyOnRefund:    "1",

	SetLowStockThreshold: "5",

	// 人机验证默认全关：没填密钥就打开只会把所有人挡在门外
	SetTurnstileEnabled:    "0",
	SetTurnstileSiteKey:    "",
	SetTurnstileSecretKey:  "",
	SetTurnstileOnLogin:    "1", // 总开关打开时，后台登录是最该保护的一个
	SetTurnstileOnOrder:    "0",
	SetTurnstileOnQuery:    "0",
	SetTurnstileOnCoupon:   "0",
	SetTurnstileWidgetSize: "normal",

	SetAdminPath: DefaultAdminPath,
	SetContacts:  "[]",
	SetBanners:   "[]",

	// 弹窗默认关闭：升级上来的站点不该突然多出一个挡在首页的框
	SetNoticePopup:       "0",
	SetNoticeForce:       "0",
	SetNoticeForceSecond: "5",

	SetMailNotifyOnPaid:     "1",
	SetMailNotifyOnDelivery: "1",
	SetMailPaidSubject:      "【{{site_name}}】订单 {{order_no}} 支付成功",
	SetMailPaidBody:         defaultPaidTemplate,
	SetMailDeliverSubject:   "【{{site_name}}】订单 {{order_no}} 已发货",
	SetMailDeliverBody:      defaultDeliverTemplate,
	SetMailManualSubject:    "【{{site_name}}】订单 {{order_no}} 已发货",
	SetMailManualBody:       defaultManualTemplate,

	SetInstalled: "0",
}

// DefaultSettings 返回首次初始化时写入的默认值（副本，调用方可安全修改）。
func DefaultSettings() map[string]string {
	out := make(map[string]string, len(defaultSettings))
	for k, v := range defaultSettings {
		out[k] = v
	}
	return out
}

const defaultPaidTemplate = `<p>您好，</p>
<p>您在 <strong>{{site_name}}</strong> 的订单已支付成功。</p>
<table>
  <tr><td>订单号</td><td>{{order_no}}</td></tr>
  <tr><td>商品名称</td><td>{{product_name}}</td></tr>
  <tr><td>购买数量</td><td>{{quantity}}</td></tr>
  <tr><td>支付金额</td><td>{{pay_amount}}</td></tr>
  <tr><td>支付时间</td><td>{{paid_at}}</td></tr>
</table>
<p>您可以随时通过订单号与邮箱查询订单：<a href="{{order_url}}">{{order_url}}</a></p>`

const defaultDeliverTemplate = `<p>您好，</p>
<p>您在 <strong>{{site_name}}</strong> 购买的商品已自动发货。</p>
<table>
  <tr><td>订单号</td><td>{{order_no}}</td></tr>
  <tr><td>商品名称</td><td>{{product_name}}</td></tr>
  <tr><td>购买数量</td><td>{{quantity}}</td></tr>
  <tr><td>支付金额</td><td>{{pay_amount}}</td></tr>
  <tr><td>购买时间</td><td>{{paid_at}}</td></tr>
</table>
<p><strong>卡密内容：</strong></p>
<pre>{{delivery_content}}</pre>
<p>请妥善保管。订单查询：<a href="{{order_url}}">{{order_url}}</a></p>`

const defaultManualTemplate = `<p>您好，</p>
<p>您在 <strong>{{site_name}}</strong> 购买的商品已由管理员发货。</p>
<table>
  <tr><td>订单号</td><td>{{order_no}}</td></tr>
  <tr><td>商品名称</td><td>{{product_name}}</td></tr>
  <tr><td>购买数量</td><td>{{quantity}}</td></tr>
  <tr><td>支付金额</td><td>{{pay_amount}}</td></tr>
</table>
<p><strong>发货内容：</strong></p>
<pre>{{delivery_content}}</pre>
<p>订单查询：<a href="{{order_url}}">{{order_url}}</a></p>`

// 邮件发送状态。
const (
	MailStatusSuccess = "success"
	MailStatusFailed  = "failed"
)

// 邮件模板标识。
const (
	MailTemplatePaid    = "paid"
	MailTemplateDeliver = "deliver"
	MailTemplateManual  = "manual"
	MailTemplateTest    = "test"
)

// EmailLog 记录每一次邮件发送结果。
//
// 邮件发送失败绝不能影响支付事务，因此这里是唯一的失败留痕。
type EmailLog struct {
	ID        uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	OrderID   uint64    `gorm:"column:order_id;index" json:"order_id"`
	OrderNo   string    `gorm:"column:order_no;size:40" json:"order_no"`
	ToEmail   string    `gorm:"column:to_email;size:190" json:"to_email"`
	Subject   string    `gorm:"column:subject;size:255" json:"subject"`
	Template  string    `gorm:"column:template;size:48" json:"template"`
	Status    string    `gorm:"column:status;size:16" json:"status"`
	Error     string    `gorm:"column:error;size:1000" json:"error"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
}

func (EmailLog) TableName() string { return "email_logs" }
