# 错误码表

所有接口（支付回调除外）返回统一结构：

```json
{ "code": 0, "message": "success", "data": {} }
```

**前端必须依赖数字 `code` 做分支判断，不要依赖 `message` 字符串** ——
文案随时可能因为国际化或表述优化而变化，`code` 才是稳定契约。

对应实现：[`server/internal/api/errcode.go`](../server/internal/api/errcode.go)

---

## 通用

| Code | 常量 | HTTP | 含义 | 前端建议 |
|---|---|---|---|---|
| `0` | `CodeSuccess` | 200 | 成功 | — |
| `40001` | `CodeBadRequest` | 400 | 请求参数错误 | 提示用户检查输入 |
| `40002` | `CodeValidation` | 400 | 参数校验失败 | 展示 message |
| `40101` | `CodeUnauthorized` | 401 | 未登录或登录已失效 | 清 token，跳登录页 |
| `40102` | `CodeTokenExpired` | 401 | 登录已过期 | 同上 |
| `40301` | `CodeForbidden` | 403 | 无权限 | 提示并返回上一页 |
| `40401` | `CodeNotFound` | 404 | 资源不存在 | — |
| `40901` | `CodeConflict` | 409 | 资源冲突 | 展示 message |
| `42901` | `CodeTooManyReqs` | 429 | 请求过于频繁 | 读 `Retry-After` 头倒计时 |
| `45301` | `CodeMaintenance` | 503 | 商城维护中 | 展示维护页 |
| `50001` | `CodeInternal` | 500 | 服务器内部错误 | 提示稍后重试 |
| `50301` | `CodeUnavailable` | 503 | 服务暂时不可用 | 提示稍后重试 |

## 分类 `441xx`

| Code | 常量 | 含义 |
|---|---|---|
| `44101` | `CodeCategoryNotFound` | 分类不存在 |
| `44102` | `CodeCategoryHasItems` | **分类下仍有商品，不允许删除**。需先转移或删除商品 |
| `44103` | `CodeCategorySlugDup` | 分类别名已存在 |

## 商品 `45xxx`

| Code | 常量 | 含义 |
|---|---|---|
| `45001` | `CodeProductNotFound` | 商品不存在 |
| `45002` | `CodeProductOffShelf` | 商品已下架 |
| `45003` | `CodeProductOutOfStk` | **库存不足**。前端应刷新商品详情展示最新库存 |
| `45004` | `CodeProductSlugDup` | 商品别名已存在 |
| `45005` | `CodeProductHasOrders` | 商品有历史订单，只能下架不能物理删除 |

## 卡密 `455xx`

| Code | 常量 | 含义 |
|---|---|---|
| `45501` | `CodeCodeNotFound` | 卡密不存在 |
| `45502` | `CodeCodeDuplicate` | 卡密重复 |
| `45503` | `CodeCodeInUse` | 卡密已被占用或已售出，**不允许删除** |

## 优惠券 `46xxx`

| Code | 常量 | 含义 |
|---|---|---|
| `46001` | `CodeCouponInvalid` | 券码不存在或不可用 |
| `46002` | `CodeCouponExpired` | 已过期 |
| `46003` | `CodeCouponNotStarted` | 尚未开始 |
| `46004` | `CodeCouponUsedUp` | 总次数已用完 |
| `46005` | `CodeCouponNotApplicable` | 不适用于当前商品 |
| `46006` | `CodeCouponMinAmount` | 未达最低消费门槛（message 含具体金额） |
| `46007` | `CodeCouponUserLimit` | 超过个人使用次数（按邮箱计） |
| `46008` | `CodeCouponDisabled` | 券已停用 |
| `46009` | `CodeCouponCodeDup` | 券码已存在（后台创建时） |

## 订单 `47xxx`

| Code | 常量 | 含义 |
|---|---|---|
| `47001` | `CodeOrderNotFound` | 订单不存在。**邮箱不匹配也返回这个码** —— 区分两者等于告诉攻击者订单号真实存在 |
| `47002` | `CodeOrderExpired` | 订单已过期 |
| `47003` | `CodeOrderStatusInvld` | 当前状态不允许该操作（状态机拒绝） |
| `47004` | `CodeOrderAlreadyPaid` | 订单已支付 |
| `47005` | `CodeOrderNotPaid` | 订单尚未支付 |
| `47006` | `CodeOrderNotDelivered` | 订单尚未发货 |
| `47007` | `CodeOrderQtyInvalid` | 购买数量不合法（message 含允许范围） |
| `47008` | `CodeShopClosed` | 商城当前不接受下单 |

## 支付 `48xxx`

| Code | 常量 | 含义 |
|---|---|---|
| `48001` | `CodePaymentChannelNotFound` | 支付方式不存在或已禁用 |
| `48002` | `CodePaymentFailed` | 发起支付失败（渠道返回错误） |
| `48003` | `CodePaymentSignInvalid` | **回调验签失败**。回调接口不返回 JSON，直接返回渠道要求的失败应答 |
| `48004` | `CodePaymentAmountMismatch` | **回调金额与订单应付金额不一致**。视为攻击或配置错误，明确拒绝 |
| `48005` | `CodePaymentDuplicate` | 重复的支付通知（已幂等跳过，不是错误） |
| `48006` | `CodePaymentNotSupported` | 该支付方式不支持此操作 |
| `48007` | `CodePaymentConfigInvalid` | 渠道配置不正确（缺必填项 / 私钥格式错误） |
| `48008` | `CodeRefundNotSupported` | 渠道不支持自动退款，请改用「人工退款」 |
| `48009` | `CodeRefundFailed` | 退款失败 |
| `48010` | `CodePaymentChannelMismatch` | 回调渠道与订单发起支付的渠道不一致（跨渠道结算尝试，已拒绝） |

## 邮件 / 设置 / 管理员 `49xxx`

| Code | 常量 | 含义 |
|---|---|---|
| `49001` | `CodeMailSendFailed` | 邮件发送失败 |
| `49002` | `CodeMailNotConfig` | 尚未配置 SMTP |
| `49003` | `CodeSettingInvalid` | 配置项不合法 |
| `49101` | `CodeAdminNotFound` | 管理员不存在 |
| `49102` | `CodeAdminDisabled` | 管理员已被禁用 |
| `49103` | `CodeAdminDupName` | 用户名已存在 |
| `49104` | `CodeAdminLastOne` | **不能删除或禁用最后一个可用管理员** |
| `49105` | `CodeAdminBadPass` | 用户名或密码错误（用户名不存在也返回这个，防枚举） |
| `49106` | `CodeAdminWeakPass` | 密码强度不足 |
| `49107` | `CodeAlreadySetup` | 系统已完成初始化 |
| `49201` | `CodeUploadInvalid` | 上传文件无效 |
| `49202` | `CodeUploadTooLarge` | 文件超过大小限制 |
| `49203` | `CodeUploadBadFormat` | 不支持的文件格式 |

---

## 前端处理示例

```ts
import { ApiError, ErrCode, shopApi } from '@/api'

try {
  await shopApi.createOrder({ product_id, quantity, email, coupon_code })
} catch (e) {
  const err = e as ApiError
  switch (err.code) {
    case ErrCode.ProductOutOfStock:
      // 库存变了，刷新商品页展示最新库存
      await reloadProduct()
      break
    case ErrCode.ShopClosed:
      showBanner('商城暂停下单')
      break
    case ErrCode.TooManyRequests:
      // message 里已包含"请 N 秒后重试"
      showToast(err.message)
      break
    default:
      showToast(err.message)
  }
}
```

## 新增错误码的规范

1. 在 `server/internal/api/errcode.go` 的对应号段里追加常量
2. 在同文件的 `codeMessages` 中补默认文案
3. 若需要特殊 HTTP 状态码，在 `httpStatusOf` 中补一条 case
4. 在本文件补一行
5. 若前端需要分支处理，在 `web/src/api/client.ts` 的 `ErrCode` 中补上

**号段约定**：`4xxxx` 客户端/业务错误，`5xxxx` 服务端错误。
业务模块各占一个万位号段，方便一眼看出是哪个模块出的问题。
