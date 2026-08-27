# 支付配置

六种支付渠道怎么申请、怎么填、怎么排错。

> 从 [README](../README.md) 拆出来的详细说明。


---

## 支付配置

所有支付渠道都在后台 → **支付方式** 中配置。

**共同要点**

1. 每个渠道有独立的**异步回调地址**，形如
   `https://你的域名/api/v1/payments/notify/{provider}/{渠道ID}`
   保存渠道后在列表里点「复制」，**填到对应支付平台的商户后台**。
2. 密钥保存后只显示脱敏值（如 `sk_l********`）。编辑时**不动它就会保留原值**。
3. 保存后点「测试」会创建一笔 1 分钱的测试单验证配置，**不会真实扣款**。
4. 同一种类型可以建多个渠道（如「易支付-主线」「易支付-备线」）。

### 易支付 V1

彩虹易支付经典接口，MD5 签名。适配绝大多数第三方易支付站点。

| 配置项 | 说明 |
|---|---|
| 支付网关地址 | 站点根地址，如 `https://pay.example.com`（**不要**带 `/submit.php`） |
| 商户 PID | 商户后台的 PID |
| 商户 KEY | 商户后台的通信密钥 |
| 下单方式 | 页面跳转（兼容性最好）/ API 下单（能直接拿二维码） |

签名规则：参数按 ASCII 升序、剔除 `sign`/`sign_type`/空值 → `a=b&c=d` → 拼接 KEY → MD5 小写。

### 易支付 V2

新版接口，`SHA256WithRSA` 签名，**支持退款**。

在商户后台 →「个人资料」→「API 信息」点击【生成商户 RSA 密钥对】，然后：

| 配置项 | 说明 |
|---|---|
| 支付网关地址 | 站点根地址 |
| 商户 ID | 商户后台的 pid |
| 签名方式 | RSA（推荐）/ MD5（兼容模式） |
| 商户私钥 | RSA 模式必填 |
| 平台公钥 | RSA 模式必填，用于验证回调签名 |

### 支付宝（官方）

在 [开放平台](https://open.alipay.com) 创建应用并签约「电脑网站支付」/「手机网站支付」。

| 配置项 | 说明 |
|---|---|
| AppID | 应用 ID |
| 应用私钥 | 密钥生成助手产出的私钥（PKCS#1 / PKCS#8 / 裸 base64 都支持） |
| 支付宝公钥 | 「接口加签方式」页面复制，也可直接粘贴支付宝公钥证书 |
| 支付方式 | 自动（PC 用电脑网站 / 手机用手机网站）/ 指定 / 当面付 |
| 沙箱环境 | 联调时开启 |

回调验签使用支付宝公钥，并额外校验 `app_id` 与渠道配置一致。

### 微信支付（官方）

需要微信支付商户号，并在商户平台完成：

1. 下载 API 证书（`apiclient_key.pem`）
2. 设置 **APIv3 密钥**（32 位）
3. 用官方 [CertificateDownloader](https://github.com/wechatpay-apiv3/CertificateDownloader) 下载**平台证书**

| 配置项 | 说明 |
|---|---|
| 商户号 mchid | 商户平台的商户号 |
| AppID | 公众号 / 小程序 / 开放平台应用的 AppID（需与商户号绑定） |
| 商户证书序列号 | 商户平台「API 安全」页面可见 |
| 商户 API 私钥 | `apiclient_key.pem` 的完整内容 |
| APIv3 密钥 | 32 位字符串，用于解密回调 |
| 微信支付平台证书 | 用于验证回调签名 |
| 支付方式 | Native 扫码 / H5 / 自动 |

> 微信会定期轮换平台证书。轮换后回调会验签失败并在支付日志中提示
> 「平台证书序列号不匹配」—— 此时重新下载证书更新配置即可。

### Stripe

| 配置项 | 说明 |
|---|---|
| Secret Key | `sk_live_...` |
| Publishable Key | 可选 |
| Webhook Secret | `whsec_...`，Dashboard → Developers → Webhooks |
| 结算币种 | ISO 4217 小写，如 `usd` / `cny` |

**必须在 Stripe Dashboard 配置 Webhook**，监听 `checkout.session.completed` 事件，
地址填渠道列表里的回调地址。

> 💡 金额数值**不做汇率换算**（换算会引入舍入误差 → 回调金额校验失败或资损）。
> 请确保结算币种与商城币种一致；需要多币种时为每种币种单独建一个渠道。

### HashPay（加密货币）

上游：https://github.com/tgdash/hashpay
支持 TRC20 / ERC20 / Base / BSC / Polygon / TON / Aptos / Solana，以及 Binance、OKX、OKPay。

**先生成一对 RSA 密钥**，公钥登记到 HashPay 商户后台，私钥填进本系统：

```bash
openssl genrsa -out hashpay_private.pem 2048
openssl rsa -in hashpay_private.pem -pubout -out hashpay_public.pem
```

| 配置项 | 说明 |
|---|---|
| 网关地址 | HashPay 站点根地址，不要带 `/api/merchant/new` |
| 商户 ID | 对应请求头 `X-Merchant-Id` |
| 商户 RSA 私钥 | `hashpay_private.pem` 内容。**同时用于请求签名与回调解密** |
| 计价币种 | 订单计价币种；用户实际付哪种币由 HashPay 收银台决定 |
| 回调后回查确认 | ⚠️ **保持开启**，原因见下 |

协议要点：请求用 `RSASSA-PKCS1-v1_5 + SHA-256` 签名，
待签名串为 `METHOD
pathname+search
timestamp
body`，时间容差 5 分钟
（**服务器时钟必须 NTP 同步**，否则所有请求都会被拒）。

> ⚠️ **为什么必须开启「回调后回查」**
>
> HashPay 的回调**只有加密、没有签名** —— 用商户公钥做 RSA-OAEP 加密。
> 而公钥按定义不是秘密。任何拿到你公钥和回调地址的人，都能自己造一份
> 完全合法的加密信封，里面写 `"status":"paid"`：解密会成功、GCM 认证也会通过，
> 因为**加密提供的是机密性，不是真实性**。
>
> 所以本适配器在解密之后，会再用**带签名的请求**回查一次订单
> （`GET /api/order/:orderId`，只有持私钥的人能发出），以服务端结果为准。
> 回调本身只当作「去查一下」的触发信号。这一步也让上层的金额校验真正有意义。

金额精度：HashPay 的 `amount` 是十进制小数（SQL `REAL`），本系统内部存「分」，
两边换算**全程走字符串**不经过 float64 —— 否则 `0.29` 会变成
`0.28999999999999998`，金额校验必然失败。

退款：HashPay 无退款接口（加密货币转账不可逆），后台会提示改用「人工退款」记账。

完整对接细节（含每条依据的上游源文件）见
[`internal/payment/hashpay/spec.md`](../server/internal/payment/hashpay/spec.md)。

> 💡 HashPay 也提供**易支付兼容接口**（上游 `routes/ezfp.ts`），
> 因此也可以直接用本项目的「易支付 V1」渠道对接它。
> 但推荐用原生接口 —— 共享密钥泄露就能伪造回调，RSA 私钥则不出本机。

### 新增其他支付渠道

1. 在 `internal/payment/` 下新建子包，实现 `payment.Provider` 接口（4 个方法）
2. 在 `init()` 里调用 `payment.Register(...)` 声明配置字段
3. 在 `internal/payment/providers/providers.go` 加一行空导入

完成。后台的配置表单会自动出现新渠道，**不需要改任何业务代码，也不需要改前端**。

---
