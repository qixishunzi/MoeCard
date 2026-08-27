# HashPay Adapter 对接说明

**上游**：https://github.com/tgdash/hashpay
（Cloudflare Workers + Hono + D1 的加密货币收款网关，支持 TRC20 / ERC20 / Base / BSC / Polygon / TON / Aptos / Solana / Binance / OKX / OKPay）

本适配器严格按上游源码实现，下面每一条都标注了依据的源文件。

---

## 1. 商户密钥模型

HashPay 侧的 `merchants` 表：

```sql
CREATE TABLE merchants (
  id TEXT PRIMARY KEY,
  public_key TEXT NOT NULL DEFAULT '',   -- 商户的 RSA 公钥
  ...
);
```

**你生成一对 RSA 密钥，把公钥登记到 HashPay 商户后台，私钥填进本系统。**

这一把私钥同时承担两个职责：

| 用途 | 方向 | 说明 |
|---|---|---|
| 请求签名 | 本站 → HashPay | 用私钥签名，HashPay 用登记的公钥验签 |
| 回调解密 | HashPay → 本站 | HashPay 用公钥加密，本站用私钥解密 |

生成命令：

```bash
openssl genrsa -out hashpay_private.pem 2048
openssl rsa -in hashpay_private.pem -pubout -out hashpay_public.pem
```

`hashpay_public.pem` 的内容登记到 HashPay，`hashpay_private.pem` 填到后台「支付方式 → HashPay → 商户 RSA 私钥」。

---

## 2. 请求签名

> 依据：`src/server/services/merchants/index.ts`

```
待签名串 = [METHOD.toUpperCase(), pathname + search, timestamp, body].join("\n")
算法     = RSASSA-PKCS1-v1_5 + SHA-256
编码     = 标准 Base64（非 URL-safe）
```

三个请求头：

| 头 | 值 |
|---|---|
| `X-Merchant-Id` | 商户 ID |
| `X-Timestamp` | Unix 秒 |
| `X-Signature` | 上述签名 |

服务端校验 `Math.abs(now - timestamp) > 300` 即拒绝，**时间容差 5 分钟**。
部署机器的时钟必须准确（NTP 同步），否则所有请求都会被拒。

⚠️ `pathname + search` 必须与真实发出的 URL 逐字符一致。多一个斜杠、
少一个 query 参数，验签就会失败，而错误信息只会是笼统的 401。

对应实现：`Provider.signedRequest()`

---

## 3. 创建订单

> 依据：`src/server/http/routes/auth.ts`（挂载在 `/api`）、`src/server/services/orders/create.ts`

```
POST {gateway}/api/merchant/new
Content-Type: application/json
```

请求体：

| 字段 | 必填 | 说明 |
|---|---|---|
| `merchantNo` | ✓ | 商户订单号。**本接口按它幂等** —— 重复提交返回已有订单 |
| `amount` | ✓ | **十进制数值**（如 `178.20`），不是最小单位。必须 > 0 |
| `currency` | | 计价币种，留空用 HashPay 默认值 |
| `description` | | 订单描述 |
| `callback` | | 异步通知地址 |
| `return_url` | | 支付完成后的跳转地址 |

响应：

```json
{
  "checkoutUrl": "https://.../checkout/ord_xxx",
  "order": { "id": "ord_xxx", "amount": 178.2, "currency": "CNY",
             "status": "pending", "expiresAt": 1800003600 },
  "reused": false
}
```

- `checkoutUrl` → 前端跳转过去（`Action: redirect`）
- `order.id` → **必须落库**（本系统存进 `orders.payment_trade_no`），
  因为查询接口只认它，不认 `merchantNo`
- `reused: true` 表示这个 `merchantNo` 已经有订单了，返回的是旧订单

> 💡 `UNIQUE(merchant, merchant_no)` 保证了幂等性，用户反复点「立即支付」
> 不会产生多笔订单。

---

## 4. 查询订单

```
GET {gateway}/api/order/:orderId
```

**`:orderId` 是 HashPay 的订单 id（`order.id`），不是 `merchantNo`。**

源码里是 `getOrder(c.env, c.req.param("orderId"))` 然后校验
`order.merchant !== merchant.id` 拒绝越权 —— 所以只能查自己的订单。

这也是本项目把 `PaymentProvider.QueryPayment` 的入参从
`orderNo string` 改成 `QueryRequest{OrderNo, TradeNo}` 的原因。

---

## 5. 异步回调 ⚠️ 重点

> 依据：`src/server/services/orders/notifications.ts`、`src/server/utils/crypto.ts`

HashPay 会 POST 到下单时传入的 `callback` 地址：

**请求头**

```
content-type: application/json
x-hashpay-encryption: RSA-OAEP-256+A256GCM
x-hashpay-merchant:   <商户 ID>
x-hashpay-timestamp:  <unix 秒>
```

**请求体（加密信封）**

```json
{
  "alg":  "RSA-OAEP-256+A256GCM",
  "key":  "<base64: 用商户公钥 RSA-OAEP(SHA-256) 加密的 AES-256 密钥>",
  "iv":   "<base64: 12 字节随机 IV>",
  "data": "<base64: AES-256-GCM 密文，认证标签附在尾部>"
}
```

**解密后**

```json
{
  "timestamp": 1800000000,
  "payload": {
    "orderId":    "ord_xxx",
    "merchantNo": "20260826ABCD",
    "amount":     178.2,
    "currency":   "CNY",
    "status":     "paid",
    "payment":    { ... 收款快照 ... }
  }
}
```

**应答**：返回任意 2xx 即视为成功（源码 `return response.ok ? null : ...`）。
失败会按 `ts + attempts * 60` 退避重试，**最多 8 次**，之后标记 `failed`。

### 为什么必须「回调后回查」

**HashPay 的回调只有加密，没有签名。**

RSA-OAEP 用的是**商户公钥**加密。而公钥按定义不是秘密 ——
它登记在 HashPay 后台，也可能出现在你的配置备份、运维文档里。

任何拿到你公钥 + 回调地址的人，都能自己造一份完全合法的加密信封，
里面写 `"status": "paid"`。解密会成功，GCM 认证也会通过 ——
因为**加密提供的是机密性，不是真实性**。

所以本适配器默认（`verify_by_query = 1`）在解密之后再做一步：

```
GET /api/order/{orderId}   ← 带 X-Signature，只有持私钥的人能发出
```

以这次查询的返回为准：状态、金额、币种全部覆盖回调里的值。
回调本身只当作「有事情发生了，去查一下」的触发信号。

配置项 `verify_by_query` 可以关掉它，但**没有正当理由不要关**。

> 这一步也顺带让上层的金额校验真正有意义：
> `OrderService.HandlePaymentSuccess` 会比对回调金额与订单应付金额，
> 而这个金额现在来自 HashPay 服务端，不是攻击者可控的回调内容。

---

## 6. 金额精度

HashPay 的 `orders.amount` 是 SQL `REAL`（十进制小数），
本系统内部统一用 `int64` 存「分」。两边换算**全程走字符串**：

```go
// 分 → 十进制。直接写 JSON 字面量，不经过 float64
func amountToJSON(cents int64) json.RawMessage {
    return json.RawMessage(utils.AmountToYuanString(cents))  // 17820 → 178.20
}

// 十进制 → 分。json.Number 保留原始字面量，纯字符串解析
func parseAmountNumber(n json.Number) (int64, error) {
    return utils.ParseAmount(n.String())
}
```

**为什么不用 float64**：`0.29` 在 float64 里是 `0.28999999999999998`，
序列化成 JSON 就会带上一串噪声，服务端 `Number()` 之后金额对不上，
回调校验必然失败。`TestAmountPrecision` 覆盖了这一点。

---

## 7. 退款

HashPay **没有退款接口**（源码中无相关路由）—— 加密货币转账本身不可逆。

`Refund()` 返回 `payment.ErrNotSupported`，后台会提示改用「人工退款」：
只在本系统记账（退款金额、时间、原因完整留痕），实际回款由你手动向用户地址转账。

---

## 8. 状态值

> 依据：`src/server/services/orders/create.ts`（初始 `'pending'`）、
> `notifications.ts`（`'paid'` 触发通知）

| 状态 | 含义 |
|---|---|
| `pending` | 订单已创建，等待收款 |
| `paid` | **已收款**，唯一会被判定为支付成功的状态 |

判定用大小写不敏感比较（`strings.EqualFold`）。

---

## 9. 接口变更时改哪里

| 变化 | 改这里 |
|---|---|
| 接口路径 | 顶部的 `pathCreate` / `pathOrder` 常量 |
| 签名串拼法 | `Provider.signedRequest()` |
| 请求头名 | `Provider.signedRequest()` 的 `headers` |
| 下单字段 | `CreatePayment()` 里的 `body` map |
| 响应字段 | `orderSummary` 结构体的 json tag |
| 回调信封 | `callbackEnvelope` 结构体 + `VerifyNotify()` |
| 回调载荷 | `callbackPayload` 结构体 |
| 状态值 | `statusPaid` / `statusPending` 常量 |
| 错误结构 | `hashpayError()` |

**绝对不要**在这里写发货、扣库存、核销优惠券的逻辑 ——
那些统一由 `OrderService.HandlePaymentSuccess()` 在事务中处理。
本包的唯一职责是「与平台通信 + 签名/验签/加解密 + 解析」。

---

## 10. 测试

`hashpay_test.go` 里用 Go **复现了 HashPay 服务端（WebCrypto）的行为**，
双向验证互通性，而不是 mock 掉加解密：

```bash
go test ./internal/payment/hashpay/ -v
```

| 测试 | 验证内容 |
|---|---|
| `TestCreatePayment_SignatureAndBody` | 下单请求能被服务端逻辑验签通过；报文字段与金额格式正确 |
| `TestAmountPrecision` | 分 ↔ 十进制往返精确，`0.29` 不会变成 `0.28999999999999998` |
| `TestVerifyNotify_Decrypt` | 能解开真实格式的 RSA-OAEP-256 + A256GCM 信封 |
| `TestVerifyNotify_Rejects` | 商户号不符 / 换密钥加密 / 密文被篡改 / 算法不符 / 字段缺失 全部拒绝 |
| `TestVerifyNotify_VerifyByQuery` | **回调说已付但服务端说未付 → 不发货**；金额以回查为准；回查失败返回 5xx 触发重试 |
| `TestQueryPayment` | 查询走平台订单号且请求带签名；兼容包一层的响应 |
| `TestNew_RejectsBadConfig` | `verify_by_query` 未配置时**默认开启**，不会静默降级 |

---

## 11. 上线前必做

1. 用**小额真实订单**跑通一次：下单 → 收银台付款 → 回调 → 发货 → 收到邮件
2. 确认服务器时钟已 NTP 同步（签名有 5 分钟容差）
3. 后台「系统日志 → 支付日志」应能看到 `create` 与 `paid` 两条记录
4. 故意用错误的私钥保存一次配置，确认后台立刻报「私钥无效」而不是等到下单才失败

---

## 附：HashPay 也兼容易支付协议

上游有 `src/server/http/routes/ezfp.ts` 与 `services/merchants/ezfp.ts`，
即 HashPay **同时提供易支付（EPay）兼容接口**。

如果你更熟悉易支付那套 MD5 签名，也可以直接用本项目的
**易支付 V1** 渠道对接 HashPay，无需使用本适配器。

两者取舍：

| | HashPay 原生（本适配器） | 易支付兼容模式 |
|---|---|---|
| 签名 | RSA-SHA256（更强） | MD5 + 共享密钥 |
| 回调真实性 | 加密 + **签名回查** | MD5 验签 |
| 配置复杂度 | 需要生成 RSA 密钥对 | 只需 PID + KEY |

推荐用原生接口 —— 共享密钥一旦泄露就能伪造回调，而 RSA 私钥不出本机。
