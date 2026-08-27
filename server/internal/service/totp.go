package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/database"
	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/utils"
)

// 后台两步验证（TOTP）。
//
// 这个系统里存着六个支付渠道的密钥和全部卡密，只靠一个密码守门是不够的。
// 启用流程刻意分两步：先拿密钥、再用一次正确的验证码换取启用，
// 避免出现"扫码失败但已经开启，从此登不进去"的死局。

// TOTPSetupResult 是开启两步验证前返回的信息。
type TOTPSetupResult struct {
	Secret string `json:"secret"` // base32 密钥，供手动输入
	URI    string `json:"uri"`    // otpauth:// 链接，前端渲染成二维码
}

// BeginTOTPSetup 生成新的 TOTP 密钥（尚未启用）。
//
// 密钥暂存在 totp_secret 里但 totp_enabled 仍为 false，
// 必须调用 EnableTOTP 并通过一次验证码校验才真正生效。
func (s *AdminService) BeginTOTPSetup(ctx context.Context, adminID uint64) (*TOTPSetupResult, error) {
	admin, err := s.repo.FindByID(ctx, nil, adminID)
	if err != nil {
		return nil, api.NewError(api.CodeAdminNotFound)
	}
	if admin.TOTPEnabled {
		return nil, api.NewErrorf(api.CodeValidation, "两步验证已经开启，如需更换请先关闭")
	}

	secret, err := utils.GenerateTOTPSecret()
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	stored, err := utils.Encrypt(secret)
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	if err := s.repo.UpdateFields(ctx, nil, adminID, map[string]any{
		"totp_secret": stored, "totp_enabled": false,
	}); err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}

	return &TOTPSetupResult{
		Secret: secret,
		URI:    utils.TOTPURI(s.settings.SiteName(), admin.Username, secret),
	}, nil
}

// EnableTOTP 用一次正确的验证码确认并开启两步验证，返回恢复码明文。
//
// 恢复码只在这一刻展示一次，之后库里只有哈希 —— 用户没抄下来就只能重置。
func (s *AdminService) EnableTOTP(ctx context.Context, adminID uint64, code, ip string) ([]string, error) {
	admin, err := s.repo.FindByID(ctx, nil, adminID)
	if err != nil {
		return nil, api.NewError(api.CodeAdminNotFound)
	}
	if admin.TOTPEnabled {
		return nil, api.NewErrorf(api.CodeValidation, "两步验证已经开启")
	}
	secret, err := utils.Decrypt(admin.TOTPSecret)
	if err != nil || secret == "" {
		return nil, api.NewErrorf(api.CodeValidation, "请先获取二维码再开启")
	}
	if !utils.VerifyTOTP(secret, code) {
		return nil, api.NewErrorf(api.CodeAdminBadTOTP, "验证码不正确，请检查手机时间是否准确")
	}

	plain, hashed, err := utils.GenerateRecoveryCodes(8)
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	raw, err := json.Marshal(hashed)
	if err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}

	if err := s.repo.UpdateFields(ctx, nil, adminID, map[string]any{
		"totp_enabled":  true,
		"totp_recovery": string(raw),
		// 开启两步验证等同于安全状态变更，让其他设备上的会话全部失效
		"token_version": admin.TokenVersion + 1,
	}); err != nil {
		return nil, api.WrapError(api.CodeInternal, err)
	}
	s.WriteLog(ctx, admin, ip, model.ActionEnableTOTP, "admin", fmt.Sprint(adminID), "开启两步验证")
	return plain, nil
}

// DisableTOTP 关闭两步验证。必须提供当前密码 —— 只有验证码不够：
// 手机被人拿到就能直接关掉，等于两步验证形同虚设。
func (s *AdminService) DisableTOTP(ctx context.Context, adminID uint64, password, ip string) error {
	admin, err := s.repo.FindByID(ctx, nil, adminID)
	if err != nil {
		return api.NewError(api.CodeAdminNotFound)
	}
	if !utils.VerifyPassword(admin.PasswordHash, password) {
		return api.NewErrorf(api.CodeAdminBadPass, "密码不正确")
	}
	if err := s.repo.UpdateFields(ctx, nil, adminID, map[string]any{
		"totp_enabled":  false,
		"totp_secret":   "",
		"totp_recovery": "",
	}); err != nil {
		return api.WrapError(api.CodeInternal, err)
	}
	s.WriteLog(ctx, admin, ip, model.ActionDisableTOTP, "admin", fmt.Sprint(adminID), "关闭两步验证")
	return nil
}

// verifySecondFactor 在登录时校验第二因子。
//
// 接受两种输入：6 位 TOTP 验证码，或一次性恢复码。
// 恢复码用掉即失效 —— 否则它就成了一个永久有效的旁路。
func (s *AdminService) verifySecondFactor(ctx context.Context, admin *model.Admin, code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return api.NewErrorf(api.CodeAdminTOTPRequired, "请输入两步验证码")
	}

	secret, err := utils.Decrypt(admin.TOTPSecret)
	if err != nil {
		// 密钥解不开（通常是换了 DATA_ENCRYPTION_KEY）：
		// 这时只能走恢复码，绝不能直接放行。
		logger.Admin().Error("TOTP 密钥解密失败", "admin_id", admin.ID, "err", err)
		secret = ""
	}
	if secret != "" && utils.VerifyTOTP(secret, code) {
		return nil
	}

	// 再试恢复码
	var hashes []string
	if admin.TOTPRecovery != "" {
		_ = json.Unmarshal([]byte(admin.TOTPRecovery), &hashes)
	}
	idx := utils.MatchRecoveryCode(hashes, code)
	if idx < 0 {
		return api.NewErrorf(api.CodeAdminBadTOTP, "验证码不正确")
	}

	// 用掉的恢复码立即作废
	remaining := append(hashes[:idx:idx], hashes[idx+1:]...)
	raw, _ := json.Marshal(remaining)
	if err := s.repo.UpdateFields(ctx, nil, admin.ID, map[string]any{
		"totp_recovery": string(raw),
	}); err != nil {
		logger.Admin().Error("作废已使用的恢复码失败", "admin_id", admin.ID, "err", err)
	}
	logger.Admin().Warn("管理员使用恢复码登录",
		"admin_id", admin.ID, "remaining", len(remaining))
	return nil
}

// TOTPStatus 返回当前管理员的两步验证状态。
type TOTPStatus struct {
	Enabled           bool `json:"enabled"`
	RecoveryRemaining int  `json:"recovery_remaining"`
}

// GetTOTPStatus 查询两步验证状态。
func (s *AdminService) GetTOTPStatus(ctx context.Context, adminID uint64) (*TOTPStatus, error) {
	admin, err := s.repo.FindByID(ctx, nil, adminID)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, api.NewError(api.CodeAdminNotFound)
		}
		return nil, api.WrapError(api.CodeInternal, err)
	}
	st := &TOTPStatus{Enabled: admin.TOTPEnabled}
	if admin.TOTPRecovery != "" {
		var hashes []string
		if json.Unmarshal([]byte(admin.TOTPRecovery), &hashes) == nil {
			st.RecoveryRemaining = len(hashes)
		}
	}
	return st, nil
}
