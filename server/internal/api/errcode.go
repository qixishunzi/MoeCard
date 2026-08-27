// Package api 定义统一的 HTTP 响应结构与错误码。
//
// 设计原则：前端只依赖数字 code 判断，不依赖 message 字符串。
// message 可以随时改文案 / 做国际化，code 是稳定契约。
package api

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrCode 是全局统一错误码。
type ErrCode int

const (
	CodeSuccess ErrCode = 0

	// 通用 4xx
	CodeBadRequest   ErrCode = 40001
	CodeValidation   ErrCode = 40002
	CodeUnauthorized ErrCode = 40101
	CodeTokenExpired ErrCode = 40102
	CodeForbidden    ErrCode = 40301
	CodeNotFound     ErrCode = 40401
	CodeConflict     ErrCode = 40901
	CodeTooManyReqs  ErrCode = 42901
	CodeMaintenance  ErrCode = 45301

	// 通用 5xx
	CodeInternal    ErrCode = 50001
	CodeUnavailable ErrCode = 50301

	// 分类 441xx
	CodeCategoryNotFound ErrCode = 44101
	CodeCategoryHasItems ErrCode = 44102
	CodeCategorySlugDup  ErrCode = 44103

	// 商品 45xxx
	CodeProductNotFound  ErrCode = 45001
	CodeProductOffShelf  ErrCode = 45002
	CodeProductOutOfStk  ErrCode = 45003
	CodeProductSlugDup   ErrCode = 45004
	CodeProductHasOrders ErrCode = 45005

	// 卡密 455xx
	CodeCodeNotFound  ErrCode = 45501
	CodeCodeDuplicate ErrCode = 45502
	CodeCodeInUse     ErrCode = 45503

	// 优惠券 46xxx
	CodeCouponInvalid       ErrCode = 46001
	CodeCouponExpired       ErrCode = 46002
	CodeCouponNotStarted    ErrCode = 46003
	CodeCouponUsedUp        ErrCode = 46004
	CodeCouponNotApplicable ErrCode = 46005
	CodeCouponMinAmount     ErrCode = 46006
	CodeCouponUserLimit     ErrCode = 46007
	CodeCouponDisabled      ErrCode = 46008
	CodeCouponCodeDup       ErrCode = 46009

	// 订单 47xxx
	CodeOrderNotFound     ErrCode = 47001
	CodeOrderExpired      ErrCode = 47002
	CodeOrderStatusInvld  ErrCode = 47003
	CodeOrderAlreadyPaid  ErrCode = 47004
	CodeOrderNotPaid      ErrCode = 47005
	CodeOrderNotDelivered ErrCode = 47006
	CodeOrderQtyInvalid   ErrCode = 47007
	CodeShopClosed        ErrCode = 47008

	// 支付 48xxx
	CodePaymentChannelNotFound ErrCode = 48001
	CodePaymentFailed          ErrCode = 48002
	CodePaymentSignInvalid     ErrCode = 48003
	CodePaymentAmountMismatch  ErrCode = 48004
	CodePaymentDuplicate       ErrCode = 48005
	CodePaymentNotSupported    ErrCode = 48006
	CodePaymentConfigInvalid   ErrCode = 48007
	CodeRefundNotSupported     ErrCode = 48008
	CodeRefundFailed           ErrCode = 48009
	CodePaymentChannelMismatch ErrCode = 48010

	// 邮件 / 设置 / 管理员 49xxx
	CodeMailSendFailed    ErrCode = 49001
	CodeMailNotConfig     ErrCode = 49002
	CodeSettingInvalid    ErrCode = 49003
	CodeAdminNotFound     ErrCode = 49101
	CodeAdminDisabled     ErrCode = 49102
	CodeAdminDupName      ErrCode = 49103
	CodeAdminLastOne      ErrCode = 49104
	CodeAdminBadPass      ErrCode = 49105
	CodeAdminWeakPass     ErrCode = 49106
	CodeAdminTOTPRequired ErrCode = 49108
	CodeAdminBadTOTP      ErrCode = 49109
	CodeAlreadySetup      ErrCode = 49107
	CodeUploadInvalid     ErrCode = 49201
	CodeUploadTooLarge    ErrCode = 49202
	CodeUploadBadFormat   ErrCode = 49203

	// 人机验证 493xx
	CodeCaptchaRequired  ErrCode = 49301 // 该场景需要人机验证但请求里没带令牌
	CodeCaptchaFailed    ErrCode = 49302 // 令牌校验未通过（过期、重放、伪造）
	CodeCaptchaMisconfig ErrCode = 49303 // 站点配置有误（密钥不对、开了却没填密钥）
)

// codeMessages 是错误码的默认中文文案。
var codeMessages = map[ErrCode]string{
	CodeSuccess: "success",

	CodeBadRequest:   "请求参数错误",
	CodeValidation:   "参数校验失败",
	CodeUnauthorized: "未登录或登录已失效",
	CodeTokenExpired: "登录已过期，请重新登录",
	CodeForbidden:    "没有权限执行该操作",
	CodeNotFound:     "资源不存在",
	CodeConflict:     "资源冲突",
	CodeTooManyReqs:  "请求过于频繁，请稍后再试",
	CodeMaintenance:  "商城正在维护中",
	CodeInternal:     "服务器内部错误",
	CodeUnavailable:  "服务暂时不可用",

	CodeCategoryNotFound: "分类不存在",
	CodeCategoryHasItems: "该分类下仍存在商品，请先转移或删除商品",
	CodeCategorySlugDup:  "分类别名已存在",

	CodeProductNotFound:  "商品不存在",
	CodeProductOffShelf:  "商品已下架",
	CodeProductOutOfStk:  "商品库存不足",
	CodeProductSlugDup:   "商品别名已存在",
	CodeProductHasOrders: "该商品存在历史订单，只能下架不能删除",

	CodeCodeNotFound:  "卡密不存在",
	CodeCodeDuplicate: "卡密重复",
	CodeCodeInUse:     "卡密已被占用或已售出，无法删除",

	CodeCouponInvalid:       "优惠券不存在或不可用",
	CodeCouponExpired:       "优惠券已过期",
	CodeCouponNotStarted:    "优惠券尚未开始",
	CodeCouponUsedUp:        "优惠券已被领完",
	CodeCouponNotApplicable: "该优惠券不适用于当前商品",
	CodeCouponMinAmount:     "未达到优惠券最低消费金额",
	CodeCouponUserLimit:     "已达到该优惠券的个人使用次数上限",
	CodeCouponDisabled:      "优惠券已停用",
	CodeCouponCodeDup:       "优惠券码已存在",

	CodeOrderNotFound:     "订单不存在",
	CodeOrderExpired:      "订单已过期",
	CodeOrderStatusInvld:  "订单状态不允许该操作",
	CodeOrderAlreadyPaid:  "订单已支付",
	CodeOrderNotPaid:      "订单尚未支付",
	CodeOrderNotDelivered: "订单尚未发货",
	CodeOrderQtyInvalid:   "购买数量不合法",
	CodeShopClosed:        "商城当前不接受下单",

	CodePaymentChannelNotFound: "支付方式不存在或已禁用",
	CodePaymentFailed:          "支付发起失败",
	CodePaymentSignInvalid:     "支付签名校验失败",
	CodePaymentAmountMismatch:  "支付金额与订单金额不一致",
	CodePaymentDuplicate:       "重复的支付通知",
	CodePaymentNotSupported:    "该支付方式暂不支持此操作",
	CodePaymentConfigInvalid:   "支付渠道配置不正确",
	CodeRefundNotSupported:     "该支付渠道暂不支持自动退款",
	CodeRefundFailed:           "退款失败",
	CodePaymentChannelMismatch: "支付回调渠道与订单不一致",

	CodeMailSendFailed:    "邮件发送失败",
	CodeMailNotConfig:     "尚未配置 SMTP",
	CodeSettingInvalid:    "配置项不合法",
	CodeAdminNotFound:     "管理员不存在",
	CodeAdminDisabled:     "管理员已被禁用",
	CodeAdminDupName:      "用户名已存在",
	CodeAdminLastOne:      "不能删除或禁用最后一个可用管理员",
	CodeAdminBadPass:      "用户名或密码错误",
	CodeAdminWeakPass:     "密码强度不足",
	CodeAdminTOTPRequired: "需要两步验证码",
	CodeAdminBadTOTP:      "两步验证码不正确",
	CodeAlreadySetup:      "系统已完成初始化",
	CodeUploadInvalid:     "上传文件无效",
	CodeUploadTooLarge:    "上传文件超过大小限制",
	CodeUploadBadFormat:   "不支持的文件格式",

	CodeCaptchaRequired:  "请先完成人机验证",
	CodeCaptchaFailed:    "人机验证未通过，请重试",
	CodeCaptchaMisconfig: "人机验证配置有误，请联系管理员",
}

// httpStatusOf 把业务错误码映射到 HTTP 状态码。
func httpStatusOf(code ErrCode) int {
	switch {
	case code == CodeSuccess:
		return http.StatusOK
	case code == CodeUnauthorized || code == CodeTokenExpired || code == CodeAdminDisabled:
		// 账号被停用同样是「这次请求没有通过身份认证」，必须是 401 ——
		// 落到默认的 400 会让客户端把它当成参数错误，不会去重新登录
		return http.StatusUnauthorized
	case code == CodeForbidden:
		return http.StatusForbidden
	case code == CodeNotFound:
		return http.StatusNotFound
	case code == CodeTooManyReqs:
		return http.StatusTooManyRequests
	case code == CodeMaintenance:
		return http.StatusServiceUnavailable
	case code == CodeConflict:
		return http.StatusConflict
	case code >= 50000:
		return http.StatusInternalServerError
	default:
		// 业务语义错误统一返回 200 之外的 400，前端按 code 分支处理。
		return http.StatusBadRequest
	}
}

// Message 返回错误码的默认文案。
func (c ErrCode) Message() string {
	if m, ok := codeMessages[c]; ok {
		return m
	}
	return "未知错误"
}

// Error 是携带业务错误码的错误类型。service 层统一返回它，
// handler 只需 api.Fail(c, err) 即可得到正确的 code/message/HTTP status。
type Error struct {
	Code    ErrCode
	Message string
	Err     error // 内部错误，仅记日志，不返回给客户端
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%d] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

// NewError 用默认文案构造业务错误。
func NewError(code ErrCode) *Error {
	return &Error{Code: code, Message: code.Message()}
}

// NewErrorf 用自定义文案构造业务错误。
func NewErrorf(code ErrCode, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// WrapError 保留内部错误用于日志，对外只暴露业务码文案。
func WrapError(code ErrCode, err error) *Error {
	return &Error{Code: code, Message: code.Message(), Err: err}
}

// WrapErrorf 同 WrapError，但自定义对外文案。
func WrapErrorf(code ErrCode, err error, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), Err: err}
}

// AsError 尝试把任意 error 还原为 *Error；不是业务错误时返回内部错误。
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: CodeInternal, Message: CodeInternal.Message(), Err: err}
}
