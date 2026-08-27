package model

import "time"

// Admin 是后台管理员。
//
// 安全要点：
//   - PasswordHash 永远是 bcrypt 哈希，禁止明文；JSON 序列化时用 "-" 屏蔽。
//   - TokenVersion 在改密码 / 强制下线时 +1，使所有已签发的 JWT 立即失效。
type Admin struct {
	Model
	Username     string `gorm:"column:username;size:64;uniqueIndex" json:"username"`
	PasswordHash string `gorm:"column:password_hash;size:255" json:"-"`
	Nickname     string `gorm:"column:nickname;size:64" json:"nickname"`
	// Avatar 是头像地址（站内 /uploads/... 或外链）。空表示用用户名首字母。
	Avatar       string     `gorm:"column:avatar;size:255" json:"avatar"`
	Status       string     `gorm:"column:status;size:16" json:"status"`
	TokenVersion int        `gorm:"column:token_version" json:"-"`
	LastLoginAt  *time.Time `gorm:"column:last_login_at" json:"last_login_at"`
	LastLoginIP  string     `gorm:"column:last_login_ip;size:64" json:"last_login_ip"`
	// TOTPSecret 落库前用主密钥加密；没有主密钥时退化为明文（并在启动时告警）。
	TOTPSecret  string `gorm:"column:totp_secret;size:255" json:"-"`
	TOTPEnabled bool   `gorm:"column:totp_enabled" json:"totp_enabled"`
	// TOTPRecovery 存的是恢复码的 sha256 哈希列表（JSON），明文只在生成时展示一次。
	TOTPRecovery string `gorm:"column:totp_recovery" json:"-"`
}

func (Admin) TableName() string { return "admins" }

// IsActive 判断管理员是否可登录。
func (a *Admin) IsActive() bool { return a.Status == StatusActive }

// AdminOperationLog 记录管理员的写操作，用于审计。
type AdminOperationLog struct {
	ID            uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	AdminID       uint64    `gorm:"column:admin_id" json:"admin_id"`
	AdminUsername string    `gorm:"column:admin_username;size:64" json:"admin_username"`
	IP            string    `gorm:"column:ip;size:64" json:"ip"`
	Action        string    `gorm:"column:action;size:64" json:"action"`
	TargetType    string    `gorm:"column:target_type;size:48" json:"target_type"`
	TargetID      string    `gorm:"column:target_id;size:64" json:"target_id"`
	Detail        string    `gorm:"column:detail" json:"detail"`
	CreatedAt     time.Time `gorm:"column:created_at" json:"created_at"`
}

func (AdminOperationLog) TableName() string { return "admin_operation_logs" }

// 管理员操作类型常量。集中定义避免各处拼字符串。
const (
	ActionLogin          = "login"
	ActionLogout         = "logout"
	ActionChangePassword = "change_password"
	ActionCreateCategory = "create_category"
	ActionUpdateCategory = "update_category"
	ActionDeleteCategory = "delete_category"
	ActionCreateProduct  = "create_product"
	ActionUpdateProduct  = "update_product"
	ActionDeleteProduct  = "delete_product"
	ActionImportCodes    = "import_codes"
	ActionDeleteCodes    = "delete_codes"
	ActionDeliverOrder   = "deliver_order"
	ActionRemarkOrder    = "remark_order"
	ActionRefundOrder    = "refund_order"
	ActionResendMail     = "resend_mail"
	ActionCreateCoupon   = "create_coupon"
	ActionUpdateCoupon   = "update_coupon"
	ActionDeleteCoupon   = "delete_coupon"
	ActionCreateChannel  = "create_payment_channel"
	ActionUpdateChannel  = "update_payment_channel"
	ActionDeleteChannel  = "delete_payment_channel"
	ActionUpdateSettings = "update_settings"
	ActionTestMail       = "test_mail"
	ActionCreateAdmin    = "create_admin"
	ActionUpdateAdmin    = "update_admin"
	ActionDeleteAdmin    = "delete_admin"
	ActionUploadFile     = "upload_file"
	ActionSetupSystem    = "setup_system"
	ActionEnableTOTP     = "enable_totp"
	ActionDisableTOTP    = "disable_totp"
	ActionExportData     = "export_data"
	ActionTestNotify     = "test_notify"
)
