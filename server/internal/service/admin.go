package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/database"
	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/utils"
)

// AdminService 处理管理员认证与账号管理。
type AdminService struct {
	db        *database.DB
	repo      *repository.AdminRepo
	logs      *repository.LogRepo
	settings  *SettingService
	jwtSecret []byte
	jwtExpire time.Duration
}

// NewAdminService 构造。
func NewAdminService(db *database.DB, repos *repository.Repositories, settings *SettingService, jwtSecret string, jwtExpire time.Duration) *AdminService {
	return &AdminService{
		db: db, repo: repos.Admin, logs: repos.Log, settings: settings,
		jwtSecret: []byte(jwtSecret), jwtExpire: jwtExpire,
	}
}

// Claims 是 JWT 载荷。
//
// TokenVersion 是关键：管理员改密码 / 被禁用时数据库里的版本号会 +1，
// 校验时发现不一致就拒绝，从而实现"一改密码，所有旧 token 立刻失效"。
type Claims struct {
	AdminID      uint64 `json:"aid"`
	Username     string `json:"usr"`
	TokenVersion int    `json:"tv"`
	jwt.RegisteredClaims
}

// LoginResult 是登录结果。
type LoginResult struct {
	Token     string       `json:"token"`
	ExpiresAt time.Time    `json:"expires_at"`
	Admin     *model.Admin `json:"admin"`
}

// Login 校验用户名密码并签发 JWT。
//
// 安全要点：
//   - 用户名不存在与密码错误返回**同一个**错误码，避免用户名枚举
//   - 用户名不存在时也执行一次假的 bcrypt 校验，让响应时间恒定
//     （否则攻击者能通过响应快慢判断用户名是否存在）
func (s *AdminService) Login(ctx context.Context, username, password, ip string) (*LoginResult, error) {
	return s.LoginWithTOTP(ctx, username, password, "", ip)
}

// LoginWithTOTP 带两步验证码的登录。
//
// 顺序很重要：先验密码再验第二因子。反过来会让攻击者在不知道密码的情况下
// 通过错误信息区分「账号是否开启了 2FA」。
func (s *AdminService) LoginWithTOTP(ctx context.Context, username, password, totpCode, ip string) (*LoginResult, error) {
	username = strings.TrimSpace(username)

	admin, err := s.repo.FindByUsername(ctx, nil, username)
	if err != nil {
		if database.IsNotFound(err) {
			// 消耗与真实校验相当的时间，抹平时序差异
			utils.VerifyPassword("$2a$12$C6UzMDM.H6dfI/f/IKcEe.0000000000000000000000000000000000", password)
			return nil, api.NewError(api.CodeAdminBadPass)
		}
		return nil, api.WrapError(api.CodeInternal, err)
	}
	if !utils.VerifyPassword(admin.PasswordHash, password) {
		logger.Admin().Warn("管理员登录失败：密码错误", "username", username, "ip", ip)
		return nil, api.NewError(api.CodeAdminBadPass)
	}
	if !admin.IsActive() {
		return nil, api.NewError(api.CodeAdminDisabled)
	}

	// 第二因子。密码已经验过，此时提示"需要验证码"不构成信息泄露。
	if admin.TOTPEnabled {
		if err := s.verifySecondFactor(ctx, admin, totpCode); err != nil {
			logger.Admin().Warn("管理员两步验证失败", "username", username, "ip", ip)
			return nil, err
		}
	}

	now := utils.NowUTC()
	if err := s.repo.UpdateFields(ctx, nil, admin.ID, map[string]any{
		"last_login_at": now,
		"last_login_ip": utils.TrimAndLimit(ip, 64),
	}); err != nil {
		logger.Admin().Warn("更新登录信息失败", "admin_id", admin.ID, "err", err)
	}
	admin.LastLoginAt = &now
	admin.LastLoginIP = ip

	token, exp, err := s.issueToken(admin)
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	s.WriteLog(ctx, admin, ip, model.ActionLogin, "admin", fmt.Sprint(admin.ID), "管理员登录")

	return &LoginResult{Token: token, ExpiresAt: exp, Admin: admin}, nil
}

func (s *AdminService) issueToken(admin *model.Admin) (string, time.Time, error) {
	now := utils.NowUTC()
	exp := now.Add(s.jwtExpire)
	claims := Claims{
		AdminID:      admin.ID,
		Username:     admin.Username,
		TokenVersion: admin.TokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprint(admin.ID),
			Issuer:    "moecard",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
			ID:        utils.RandomHex(8),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("签发 token 失败: %w", err)
	}
	return token, exp, nil
}

// ParseToken 校验并解析 JWT，同时确认管理员仍然可用。
func (s *AdminService) ParseToken(ctx context.Context, tokenStr string) (*model.Admin, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		// 必须显式校验签名算法：否则攻击者可以把 alg 改成 none 绕过验签
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	}, jwt.WithIssuer("moecard"), jwt.WithLeeway(30*time.Second))

	if err != nil {
		if strings.Contains(err.Error(), "expired") {
			return nil, api.NewError(api.CodeTokenExpired)
		}
		return nil, api.NewError(api.CodeUnauthorized)
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, api.NewError(api.CodeUnauthorized)
	}

	admin, err := s.repo.FindByID(ctx, nil, claims.AdminID)
	if err != nil {
		return nil, api.NewError(api.CodeUnauthorized)
	}
	if !admin.IsActive() {
		// 用 CodeUnauthorized 而不是 CodeAdminDisabled：
		// 这里是「令牌还能不能用」的判定，前端靠这个码决定清 token 跳登录页。
		// 换成业务码的话，被停用的管理员会卡在一个每个接口都报错、
		// 却始终不提示重新登录的空壳后台里。停用的原因写在 message 里。
		return nil, api.NewErrorf(api.CodeUnauthorized, "账号已被停用，请联系其他管理员")
	}
	// 版本号不一致说明密码已改、已登出或被强制下线
	if admin.TokenVersion != claims.TokenVersion {
		return nil, api.NewError(api.CodeTokenExpired)
	}
	return admin, nil
}

// ChangePassword 修改自己的密码。修改后所有已签发 token 立即失效。
func (s *AdminService) ChangePassword(ctx context.Context, adminID uint64, oldPwd, newPwd, ip string) error {
	admin, err := s.repo.FindByID(ctx, nil, adminID)
	if err != nil {
		return api.NewError(api.CodeAdminNotFound)
	}
	if !utils.VerifyPassword(admin.PasswordHash, oldPwd) {
		return api.NewErrorf(api.CodeAdminBadPass, "原密码不正确")
	}
	if err := utils.ValidatePasswordStrength(newPwd); err != nil {
		return api.NewErrorf(api.CodeAdminWeakPass, "%s", err.Error())
	}
	hash, err := utils.HashPassword(newPwd)
	if err != nil {
		return api.WrapError(api.CodeInternal, err)
	}
	if err := s.repo.UpdateFields(ctx, nil, adminID, map[string]any{
		"password_hash": hash,
		"token_version": admin.TokenVersion + 1,
	}); err != nil {
		return api.WrapError(api.CodeInternal, err)
	}
	s.WriteLog(ctx, admin, ip, model.ActionChangePassword, "admin", fmt.Sprint(adminID), "修改密码")
	return nil
}

// Logout 使该管理员当前已签发的全部令牌立即失效。
//
// JWT 是无状态的，服务端不留会话，所以"登出"如果只让前端清掉 localStorage，
// 那张令牌在服务端依然有效到过期为止 —— 在公用电脑上点了登出走人，
// 之前被复制走的令牌照样能进后台。这不是理论风险，是登出这个动作的全部意义所在。
//
// 用 TokenVersion +1 实现，而不是维护一张令牌黑名单：
// 黑名单要额外的表、要定时清理过期条目，而这套系统本来就用 TokenVersion
// 处理改密码和禁用账号，复用它零新增状态。
//
// 代价是同一账号在其它设备上的登录也会一起失效。对这套系统是可以接受的
// —— 它面向的是单管理员的小店，而"退出后所有设备都要重新登录"本身也是
// 更安全的默认行为。调用方需要把这一点写进提示文案。
func (s *AdminService) Logout(ctx context.Context, adminID uint64) error {
	admin, err := s.repo.FindByID(ctx, nil, adminID)
	if err != nil {
		if database.IsNotFound(err) {
			return nil // 账号已被删，令牌本来就用不了了
		}
		return api.WrapError(api.CodeInternal, err)
	}
	if err := s.repo.UpdateFields(ctx, nil, adminID, map[string]any{
		"token_version": admin.TokenVersion + 1,
	}); err != nil {
		return api.WrapError(api.CodeInternal, err)
	}
	return nil
}

// normalizeAvatar 校验头像地址。
//
// 只收站内相对路径和 http(s) 外链。放开 javascript: / data: 之类的伪协议
// 等于给后台每个页面挂一个点击即执行的 XSS —— 头像是要渲染成 <img src> 的。
func normalizeAvatar(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", nil // 明确清空
	}
	if len(v) > 255 {
		return "", api.NewErrorf(api.CodeValidation, "头像地址过长")
	}
	if !strings.HasPrefix(v, "/") &&
		!strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
		return "", api.NewErrorf(api.CodeValidation,
			"头像地址必须是站内路径或 http(s) 链接")
	}
	return v, nil
}

// AdminInput 是创建/更新管理员的入参。
type AdminInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
	Status   string `json:"status"`
	// Avatar 用指针区分"没提交"和"提交了空值"：
	// 前者保持原样，后者是站长明确要清掉头像。
	Avatar *string `json:"avatar"`
}

// CreateAdmin 创建管理员。
func (s *AdminService) CreateAdmin(ctx context.Context, in *AdminInput) (*model.Admin, error) {
	username := strings.TrimSpace(in.Username)
	if len(username) < 3 || len(username) > 32 {
		return nil, api.NewErrorf(api.CodeValidation, "用户名长度需在 3-32 之间")
	}
	if err := utils.ValidatePasswordStrength(in.Password); err != nil {
		return nil, api.NewErrorf(api.CodeAdminWeakPass, "%s", err.Error())
	}

	var out model.Admin
	err := s.db.Tx(ctx, func(tx *gorm.DB) error {
		dup, err := s.repo.UsernameExists(ctx, tx, username, 0)
		if err != nil {
			return err
		}
		if dup {
			return api.NewError(api.CodeAdminDupName)
		}
		hash, err := utils.HashPassword(in.Password)
		if err != nil {
			return err
		}
		status := model.StatusActive
		if in.Status == model.StatusDisabled {
			status = model.StatusDisabled
		}
		avatar := ""
		if in.Avatar != nil {
			if avatar, err = normalizeAvatar(*in.Avatar); err != nil {
				return err
			}
		}
		out = model.Admin{
			Username:     username,
			PasswordHash: hash,
			Nickname:     utils.TrimAndLimit(in.Nickname, 60),
			Avatar:       avatar,
			Status:       status,
			TokenVersion: 1,
		}
		return s.repo.Create(ctx, tx, &out)
	})
	if err != nil {
		return nil, wrapServiceErr(err)
	}
	return &out, nil
}

// ProfileInput 是"改自己的资料"的入参。
//
// 和 AdminInput 分开：改自己的时候不该顺手能改用户名、密码和启用状态 ——
// 那三样各有各的路径（改密码要验旧密码，停用自己更是没有意义）。
type ProfileInput struct {
	Nickname *string `json:"nickname"`
	Avatar   *string `json:"avatar"`
}

// UpdateProfile 管理员改自己的昵称和头像。
func (s *AdminService) UpdateProfile(ctx context.Context, id uint64, in *ProfileInput) (*model.Admin, error) {
	fields := map[string]any{}
	if in.Nickname != nil {
		fields["nickname"] = utils.TrimAndLimit(*in.Nickname, 60)
	}
	if in.Avatar != nil {
		av, err := normalizeAvatar(*in.Avatar)
		if err != nil {
			return nil, err
		}
		fields["avatar"] = av
	}
	if len(fields) > 0 {
		if err := s.repo.UpdateFields(ctx, nil, id, fields); err != nil {
			return nil, api.WrapError(api.CodeInternal, err)
		}
	}
	admin, err := s.repo.FindByID(ctx, nil, id)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, api.NewError(api.CodeAdminNotFound)
		}
		return nil, api.WrapError(api.CodeInternal, err)
	}
	return admin, nil
}

// UpdateAdmin 更新管理员。
func (s *AdminService) UpdateAdmin(ctx context.Context, id uint64, in *AdminInput) (*model.Admin, error) {
	var out model.Admin
	err := s.db.Tx(ctx, func(tx *gorm.DB) error {
		admin, err := s.repo.FindByID(ctx, tx, id)
		if err != nil {
			if database.IsNotFound(err) {
				return api.NewError(api.CodeAdminNotFound)
			}
			return err
		}

		fields := map[string]any{}
		if u := strings.TrimSpace(in.Username); u != "" && u != admin.Username {
			if len(u) < 3 || len(u) > 32 {
				return api.NewErrorf(api.CodeValidation, "用户名长度需在 3-32 之间")
			}
			dup, err := s.repo.UsernameExists(ctx, tx, u, id)
			if err != nil {
				return err
			}
			if dup {
				return api.NewError(api.CodeAdminDupName)
			}
			fields["username"] = u
			admin.Username = u
		}
		if in.Nickname != "" {
			fields["nickname"] = utils.TrimAndLimit(in.Nickname, 60)
		}
		if in.Avatar != nil {
			av, err := normalizeAvatar(*in.Avatar)
			if err != nil {
				return err
			}
			fields["avatar"] = av
			admin.Avatar = av
		}
		if in.Password != "" {
			if err := utils.ValidatePasswordStrength(in.Password); err != nil {
				return api.NewErrorf(api.CodeAdminWeakPass, "%s", err.Error())
			}
			hash, err := utils.HashPassword(in.Password)
			if err != nil {
				return err
			}
			fields["password_hash"] = hash
			fields["token_version"] = admin.TokenVersion + 1
		}
		if in.Status != "" && in.Status != admin.Status {
			if in.Status == model.StatusDisabled {
				// 不允许禁用最后一个可用管理员，否则没人能登录后台了
				n, err := s.repo.CountActive(ctx, tx, id)
				if err != nil {
					return err
				}
				if n == 0 {
					return api.NewError(api.CodeAdminLastOne)
				}
				// 禁用时同步作废其已签发的 token
				fields["token_version"] = admin.TokenVersion + 1
			}
			fields["status"] = in.Status
			admin.Status = in.Status
		}

		if len(fields) > 0 {
			if err := s.repo.UpdateFields(ctx, tx, id, fields); err != nil {
				return err
			}
		}
		updated, err := s.repo.FindByID(ctx, tx, id)
		if err != nil {
			return err
		}
		out = *updated
		return nil
	})
	if err != nil {
		return nil, wrapServiceErr(err)
	}
	return &out, nil
}

// DeleteAdmin 删除管理员。
func (s *AdminService) DeleteAdmin(ctx context.Context, id, operatorID uint64) error {
	if id == operatorID {
		return api.NewErrorf(api.CodeForbidden, "不能删除当前登录的账号")
	}
	return wrapServiceErr(s.db.Tx(ctx, func(tx *gorm.DB) error {
		if _, err := s.repo.FindByID(ctx, tx, id); err != nil {
			if database.IsNotFound(err) {
				return api.NewError(api.CodeAdminNotFound)
			}
			return err
		}
		n, err := s.repo.CountActive(ctx, tx, id)
		if err != nil {
			return err
		}
		if n == 0 {
			return api.NewError(api.CodeAdminLastOne)
		}
		return s.repo.Delete(ctx, tx, id)
	}))
}

// ListAdmins 分页查询管理员。
func (s *AdminService) ListAdmins(ctx context.Context, offset, limit int) ([]model.Admin, int64, error) {
	list, total, err := s.repo.List(ctx, nil, offset, limit)
	if err != nil {
		return nil, 0, api.WrapError(api.CodeInternal, err)
	}
	return list, total, nil
}

// GetByID 查询管理员。
func (s *AdminService) GetByID(ctx context.Context, id uint64) (*model.Admin, error) {
	a, err := s.repo.FindByID(ctx, nil, id)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, api.NewError(api.CodeAdminNotFound)
		}
		return nil, api.WrapError(api.CodeInternal, err)
	}
	return a, nil
}

// NeedsSetup 判断系统是否还未初始化（没有任何管理员）。
func (s *AdminService) NeedsSetup(ctx context.Context) (bool, error) {
	n, err := s.repo.Total(ctx, nil)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

// SetupInput 是首次初始化的入参。
type SetupInput struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	SiteName string `json:"site_name"`
	Email    string `json:"email"`
}

// Setup 首次初始化：创建第一个管理员并写入商城名称。
//
// 只有在系统完全没有管理员时才允许调用 —— 否则任何人都能重置商城。
func (s *AdminService) Setup(ctx context.Context, in *SetupInput, ip string) (*model.Admin, error) {
	need, err := s.NeedsSetup(ctx)
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	if !need {
		return nil, api.NewError(api.CodeAlreadySetup)
	}

	admin, err := s.CreateAdmin(ctx, &AdminInput{
		Username: in.Username,
		Password: in.Password,
		Nickname: "超级管理员",
		Status:   model.StatusActive,
	})
	if err != nil {
		return nil, err
	}

	updates := map[string]string{model.SetInstalled: "1"}
	if name := strings.TrimSpace(in.SiteName); name != "" {
		updates[model.SetSiteName] = utils.TrimAndLimit(name, 60)
	}
	if err := s.settings.Update(ctx, updates); err != nil {
		logger.L().Error("初始化写入商城配置失败", "err", err)
	}

	s.WriteLog(ctx, admin, ip, model.ActionSetupSystem, "system", "0", "系统初始化")
	logger.L().Info("系统初始化完成", "admin", admin.Username)
	return admin, nil
}

// WriteLog 记录管理员操作日志。
//
// 写日志失败绝不影响主流程 —— 只记应用日志并继续。
func (s *AdminService) WriteLog(ctx context.Context, admin *model.Admin, ip, action, targetType, targetID, detail string) {
	l := &model.AdminOperationLog{
		IP:         utils.TrimAndLimit(ip, 64),
		Action:     action,
		TargetType: targetType,
		TargetID:   utils.TrimAndLimit(targetID, 60),
		Detail:     detail,
	}
	if admin != nil {
		l.AdminID, l.AdminUsername = admin.ID, admin.Username
	}
	if err := s.logs.CreateAdminLog(ctx, nil, l); err != nil {
		logger.Admin().Error("写入管理员操作日志失败", "action", action, "err", err)
	}
}

// ListOperationLogs 查询操作日志。
func (s *AdminService) ListOperationLogs(ctx context.Context, q repository.AdminLogQuery) ([]model.AdminOperationLog, int64, error) {
	list, total, err := s.logs.ListAdminLogs(ctx, nil, q)
	if err != nil {
		return nil, 0, api.WrapError(api.CodeInternal, err)
	}
	return list, total, nil
}
