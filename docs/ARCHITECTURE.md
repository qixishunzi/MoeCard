# MoeCard 架构设计文档

> 阶段 1 产出：系统架构 / 目录结构 / ER 设计 / 表结构 / 订单状态机 / 支付架构 / 自动发货 / 优惠券 / API 路由 / 安全设计

---

## 1. 系统整体架构

```
                    ┌──────────────────────────────────────────┐
                    │            浏览器 (PC / Mobile)           │
                    │   Vue3 SPA：商城前台 (/)  管理后台 (/admin)│
                    └───────────────┬──────────────────────────┘
                                    │ HTTPS / JSON
                    ┌───────────────▼──────────────────────────┐
                    │        Go 单体二进制 (Gin)                 │
                    │  ┌────────────────────────────────────┐  │
                    │  │ Middleware                          │  │
                    │  │ RequestID / Recover / CORS / Log    │  │
                    │  │ RateLimit / JWT Auth / Maintenance  │  │
                    │  └───────────────┬────────────────────┘  │
                    │  ┌───────────────▼────────────────────┐  │
                    │  │ Handler  (HTTP 编解码 + 参数校验)     │  │
                    │  └───────────────┬────────────────────┘  │
                    │  ┌───────────────▼────────────────────┐  │
                    │  │ Service  (业务编排 + 事务 + 状态机)   │  │
                    │  │ Order / Product / Coupon / Code     │  │
                    │  │ Payment / Mail / Setting / Admin    │  │
                    │  └───┬───────────┬──────────┬─────────┘  │
                    │      │           │          │            │
                    │  ┌───▼────┐ ┌────▼─────┐ ┌──▼─────────┐  │
                    │  │Repo层  │ │Payment   │ │Mail/Storage│  │
                    │  │(GORM)  │ │Provider  │ │ Provider   │  │
                    │  └───┬────┘ └────┬─────┘ └──┬─────────┘  │
                    │      │           │          │            │
                    │  ┌───▼──────────────────────────────┐    │
                    │  │ database 层：驱动差异统一封装      │    │
                    │  │ Tx() / LockRow() / Retry / Migrate│    │
                    │  └───┬──────────────────────────────┘    │
                    │      │                                    │
                    │  embed:web/dist  (SPA fallback)          │
                    └──────┼──────────────┬─────────────┬──────┘
                           │              │             │
                    ┌──────▼─────┐  ┌─────▼──────┐  ┌───▼─────┐
                    │SQLite/MySQL│  │ 支付平台    │  │  SMTP   │
                    └────────────┘  └────────────┘  └─────────┘
                                          ▲
                                          │ 异步回调 (验签)
```

### 关键设计决策

| 决策 | 选择 | 理由 |
|---|---|---|
| SQLite 驱动 | `github.com/glebarez/sqlite`（纯 Go，基于 modernc） | 免 CGO，交叉编译 / Alpine 容器 / 单文件二进制都能跑 |
| 支付 SDK | **不使用任何官方 SDK**，全部用 stdlib crypto 手写签名 | 低依赖、可审计、不受 SDK 版本破坏 |
| 金额 | `int64` 最小货币单位（分） | 杜绝浮点精度问题 |
| 库存 | **下单即预占**，过期释放 | 用户付了钱一定有货；符合"过期需释放库存"要求 |
| 事务并发 | SQLite 全局写锁 + MySQL `SELECT FOR UPDATE`，统一封装在 `database.Tx` | 业务层零 `if sqlite {}` 分支 |
| 迁移 | 手写 SQL，分 `migrations/sqlite` 与 `migrations/mysql`，`schema_migrations` 记录版本 | 生产可控，不依赖 AutoMigrate 猜测 |
| 前端 | 单 Vue 工程，路由区分前台/后台，后台路由懒加载独立 chunk | 首页体积小，部署简单 |
| 部署 | Go `embed` 前端 dist → 单二进制 | `docker compose up -d` 即可 |

---

## 2. 项目目录结构

```
MoeCard/
├── docs/
│   ├── ARCHITECTURE.md        本文件
│   ├── ERRORS.md              错误码表
│   └── openapi.yaml           OpenAPI 3.0 规范（手写，随代码维护）
├── server/
│   ├── cmd/
│   │   ├── server/main.go     API 服务入口
│   │   └── migrate/main.go    独立迁移命令
│   ├── internal/
│   │   ├── api/               统一 Response / 错误码 / 路由注册
│   │   ├── handler/
│   │   │   ├── public/        前台 handler
│   │   │   └── admin/         后台 handler
│   │   ├── middleware/        JWT / RateLimit / Log / CORS / Recover
│   │   ├── service/           业务逻辑（事务边界在这里）
│   │   ├── repository/        数据访问（只做 CRUD + 查询）
│   │   ├── model/             GORM Model + 常量 + 状态机
│   │   ├── payment/           PaymentProvider 抽象 + 各渠道 Adapter
│   │   │   ├── provider.go    接口定义
│   │   │   ├── registry.go    provider 注册表
│   │   │   ├── yipayv1/  yipayv2/  alipay/  wechat/  stripe/  hashpay/
│   │   ├── mail/              Mailer 接口 + SMTP 实现 + 模板
│   │   ├── storage/           StorageProvider 接口 + Local 实现
│   │   ├── config/            配置加载（yaml + env）
│   │   ├── database/          连接 / 事务 / 锁 / 迁移执行器
│   │   ├── logger/            slog 结构化日志
│   │   ├── web/               embed 前端 dist + SPA fallback
│   │   └── utils/             订单号 / 随机串 / 加密 / 分页 / 时间
│   ├── migrations/{sqlite,mysql}/*.sql
│   ├── config/config.example.yaml
│   ├── storage/uploads/
│   └── go.mod
├── web/                       Vue3 + TS + Vite（前台 + 后台）
├── Dockerfile
├── docker-compose.yml
├── .env.example
└── README.md
```

**分层职责红线**

- `handler` 只做：绑定参数 → 调 service → 写 Response。**禁止**出现 `db.` 或业务分支。
- `service` 是唯一的事务边界。**禁止**出现 `c.JSON` 或 `gin.Context`。
- `repository` 只做数据访问。**禁止**开启事务、**禁止**调用其他 service。
- `payment/*` 只做：与支付平台通信、签名、验签、解析回调。**禁止**发货、核销优惠券、改订单状态。

---

## 3. 数据库 ER 设计

```
  categories 1───* products *───1 (soft delete 保留历史)
                     │
                     ├──1───* product_codes        (卡密，自动发货库存)
                     │
                     └──1───* order_items ────*───1 orders
                                                    │
                              coupons 1───────*─────┤ (coupon_id)
                                 │                  │
                                 ├─*─ coupon_products *─1 products
                                 └─*─ coupon_usages  *──1 orders
                                                    │
                     payment_channels 1────────*────┤
                                 │                  │
                                 └──*─ payment_logs *┘
                                                    │
                                 email_logs *───────┘

  admins 1───* admin_operation_logs
  system_settings (k/v)      schema_migrations
```

**关系要点**

- `orders` ↔ `order_items`：一对多。当前前台"立即购买"只产生 1 个 item，但结构已为购物车 / 多商品订单预留。
- `order_items` 保存**商品快照**（`product_name` / `product_price` / `delivery_type` / `product_cover`），商品被软删也不影响历史订单。
- `product_codes` 独立表，卡密与商品解耦；`order_id` + `order_item_id` 指向消费它的订单。
- `coupon_products` 多对多，禁止把商品 ID 数组塞 varchar。
- `coupon_usages` 记录真实核销（支付成功后写入），`(coupon_id, order_id)` 唯一 → 天然幂等；`email` 字段支撑 `per_user_limit`。
- `payment_channels` 与 `provider` 是不同概念：同一 provider 可建多个渠道（易支付主线/备线、Stripe US/HK）。

---

## 4. 完整数据库表结构

金额字段一律 `BIGINT`，单位 = 最小货币单位（人民币=分）。时间一律存 **UTC**。

### admins
| 字段 | 类型 | 说明 |
|---|---|---|
| id | BIGINT PK AI | |
| username | VARCHAR(64) UNIQUE | |
| password_hash | VARCHAR(255) | bcrypt cost 12 |
| nickname | VARCHAR(64) | |
| status | VARCHAR(16) | `active` / `disabled` |
| token_version | INT | 改密码 +1，旧 JWT 立即失效 |
| last_login_at | DATETIME NULL | |
| last_login_ip | VARCHAR(64) | |
| created_at / updated_at | DATETIME | |

### categories
`id, name, slug(UNIQUE), description, sort, status(active/disabled), created_at, updated_at`
删除保护：分类下存在未软删商品时拒绝删除。

### products
| 字段 | 类型 | 说明 |
|---|---|---|
| id | BIGINT PK | |
| category_id | BIGINT IDX | |
| name / slug(UNIQUE) / cover / description | | `description` 支持富文本，输出前后端双重净化 |
| price / original_price | BIGINT | 分 |
| stock | BIGINT | 仅 `manual` 使用；`-1` = 无限 |
| delivery_type | VARCHAR(16) | `auto` / `manual` |
| status | VARCHAR(16) | `on` 上架 / `off` 下架 |
| sort | INT | 越大越前 |
| sales_count | BIGINT | 支付成功累加 |
| is_recommend | TINYINT | 首页推荐 |
| deleted_at | DATETIME NULL IDX | soft delete |
| created_at / updated_at | | |

### product_codes（卡密）
| 字段 | 类型 | 说明 |
|---|---|---|
| id | BIGINT PK | |
| product_id | BIGINT IDX | |
| content | VARCHAR(500) | 卡密正文 |
| content_hash | CHAR(64) IDX | sha256(product_id + content)，`(product_id, content_hash)` UNIQUE → 同商品去重 |
| status | VARCHAR(16) IDX | `unused` / `locked` / `sold` |
| order_id / order_item_id | BIGINT NULL IDX | |
| locked_at / sold_at | DATETIME NULL | |
| created_at | DATETIME | |

复合索引 `(product_id, status, id)` —— 领卡密热路径。

### orders
| 字段 | 类型 | 说明 |
|---|---|---|
| id | BIGINT PK | |
| order_no | VARCHAR(40) UNIQUE | `yyyyMMddHHmmss` + 10 位随机 Base32 |
| query_token | CHAR(32) UNIQUE IDX | 免邮箱直查 token |
| email | VARCHAR(190) IDX | |
| original_amount / discount_amount / pay_amount | BIGINT | 分 |
| coupon_id | BIGINT NULL | |
| coupon_code | VARCHAR(64) | 快照 |
| payment_channel_id | BIGINT NULL | |
| payment_method | VARCHAR(64) | 快照：渠道名 |
| payment_provider | VARCHAR(32) | 快照：provider key |
| payment_trade_no | VARCHAR(128) IDX | 平台流水号 |
| status | VARCHAR(24) IDX | 见状态机 |
| delivery_type | VARCHAR(16) | 订单级快照 |
| delivery_content | TEXT | 手动发货内容 |
| stock_reserved | TINYINT | 是否已预占库存（过期时决定是否释放） |
| needs_attention | TINYINT | 超卖/异常需人工处理 |
| remark | TEXT | 管理员备注 |
| client_ip | VARCHAR(64) | |
| refund_amount | BIGINT | |
| refund_reason | VARCHAR(255) | |
| refunded_at | DATETIME NULL | |
| paid_at / delivered_at / expired_at | DATETIME NULL | |
| created_at / updated_at | | |

### order_items
`id, order_id IDX, product_id, product_name, product_slug, product_cover, product_price(BIGINT), delivery_type, quantity, subtotal(BIGINT), delivery_content(TEXT), created_at`

### coupons
| 字段 | 说明 |
|---|---|
| code UNIQUE / name | |
| type | `fixed`（固定减）/ `percent`（百分比） |
| value | fixed → 分；percent → **万分比**（9 折 = 9000），避免小数 |
| min_amount | 最低消费（分），0=不限 |
| max_discount | percent 时最大优惠（分），0=不限 |
| scope | `all` / `products` |
| usage_limit | 总次数，0=不限 |
| used_count | 已核销次数 |
| per_user_limit | 单邮箱次数，0=不限 |
| start_at / expire_at | NULL=不限 |
| status | `active` / `disabled` |

> **为何 percent 用万分比**：`9 折` 若存 `0.9` 就回到浮点。存 `9000`，计算 `amount * 9000 / 10000` 全整数运算。

### coupon_products
`id, coupon_id IDX, product_id IDX`，`(coupon_id, product_id)` UNIQUE

### coupon_usages
`id, coupon_id IDX, order_id, email IDX, discount_amount, created_at`，`(coupon_id, order_id)` **UNIQUE** ← 幂等核心

### payment_channels
`id, name, provider, config(TEXT/JSON), status(enabled/disabled), sort, remark, created_at, updated_at`

### payment_logs
`id, order_id IDX, order_no, channel_id, provider, trade_no, event, amount, status, request_data(TEXT), response_data(TEXT), client_ip, created_at`
写入前经过 `SanitizeSensitive()` 过滤 key/secret/private_key/token/password。

### system_settings
`id, key UNIQUE, value(TEXT), is_secret TINYINT, updated_at`

### admin_operation_logs
`id, admin_id, admin_username, ip, action, target_type, target_id, detail(TEXT), created_at`

### email_logs
`id, order_id, to_email, subject, template, status(success/failed), error, created_at`

### schema_migrations
`version VARCHAR(64) PK, name, applied_at`

---

## 5. 订单状态机

```
                 ┌──────────┐
   创建订单 ───▶ │ pending  │ 待付款（已预占库存）
                 └────┬─────┘
        用户点支付      │            超时 / 用户取消
         ┌─────────────┼──────────────┬──────────────┐
         ▼             │              ▼              ▼
    ┌─────────┐        │        ┌──────────┐   ┌───────────┐
    │ paying  │────────┘        │ expired  │   │ cancelled │
    └────┬────┘  (可退回 pending) └──────────┘   └───────────┘
         │  支付回调验签通过 → HandlePaymentSuccess()          （释放库存）
         ▼
    ┌─────────┐
    │  paid   │ 已支付（钱已收到）
    └────┬────┘
         │
    ┌────┴──────────────────────┐
    │ delivery_type=auto        │ delivery_type=manual
    ▼                           ▼
┌───────────┐            ┌──────────────────┐
│ completed │◀───────────│ waiting_delivery │  管理员填写发货内容
└─────┬─────┘  确认发货   └────────┬─────────┘
      │                            │
      └──────────┬─────────────────┘
                 ▼
           ┌───────────┐
           │ refunded  │  (管理员退款 / 渠道退款)
           └───────────┘
```

**合法转换表**（`model/order_status.go` 中以 map 硬编码，非法转换直接返回 `ORDER_STATUS_INVALID`）

| From | 允许 To |
|---|---|
| pending | paying, paid, cancelled, expired |
| paying | pending, paid, cancelled, expired |
| paid | waiting_delivery, completed, refunded |
| waiting_delivery | completed, refunded |
| completed | refunded |
| cancelled / expired / refunded | *(终态，无出边)* |

> `completed → pending` 之类的回退被表拒绝，且所有变更走 `order.TransitionTo(next)`，不允许直接写 `o.Status = x`。

### 订单过期与"迟到回调"

- 定时任务（默认 60s）扫描 `status IN (pending, paying) AND expired_at < now()` → 置 `expired` 并释放预占库存。
- **迟到回调**：回调进入 `HandlePaymentSuccess` 时若订单已 `expired`：
  1. 该状态不在 `paid` 的合法前驱里 → 走「复活」分支；
  2. 在同一事务内**重新尝试占用库存/卡密**；
  3. 成功 → 恢复为 `paid` 并正常发货；
  4. 失败（已售罄）→ 置 `paid` + `waiting_delivery` + `needs_attention=1` + 写备注，后台 Dashboard 红色告警，人工补货或退款。
  
  **绝不丢单、绝不静默吞掉已收到的钱。**

---

## 6. 支付架构

```go
type PaymentProvider interface {
    Key() string
    CreatePayment(ctx context.Context, req PaymentRequest) (*PaymentResponse, error)
    VerifyNotify(ctx context.Context, req NotifyRequest) (*NotifyResult, error)
    QueryPayment(ctx context.Context, orderNo string) (*PaymentStatus, error)
    Refund(ctx context.Context, req RefundRequest) (*RefundResult, error)
    ConfigSchema() []ConfigField          // 驱动后台表单 + 脱敏规则
}
```

```
HTTP 回调  POST /api/v1/payments/notify/:provider/:channel_id
    │
    ▼
NotifyHandler
    ├─ 按 channel_id 取渠道配置（provider 必须匹配 URL 中的 :provider）
    ├─ Registry.Build(provider, config) → PaymentProvider 实例
    ├─ provider.VerifyNotify()   ← 唯一的"这笔支付是否真的成功"判定点
    │     └─ 失败 → 记 payment_log(event=notify_invalid) → 返回错误，不改任何业务数据
    └─ 成功 → OrderService.HandlePaymentSuccess(orderNo, tradeNo, paidAmount, provider, channelID)
                  └─ 统一事务（见 §23 流程）
    ▼
provider.NotifyResponse()  → 各平台要求的应答体（易支付 "success"、微信 JSON、Stripe 200）
```

**Adapter 只做 4 件事**：拼参数、签名、发请求、验签解析。任何一个 adapter 里出现"发卡密""改库存"都是架构违规。

### 各渠道要点

| Provider | 下单方式 | 签名 | 回调验证 |
|---|---|---|---|
| `yipay_v1` | `submit.php` 表单跳转 / `mapi.php` JSON | MD5：ASCII 升序、剔除 `sign`/`sign_type`/空值、`a=b&c=d`+KEY，小写 | GET 参数 MD5 验签 + `trade_status=TRADE_SUCCESS`，应答 `success` |
| `yipay_v2` | `POST /api/pay/create`（form-urlencoded，JSON 响应） | 默认 `RSA`(SHA256WithRSA, base64)，可切 `MD5` | 平台公钥验签，应答 `success` |
| `alipay` | `alipay.trade.page.pay` / `wap.pay` 跳转 | RSA2(SHA256WithRSA) | 支付宝公钥验签 + `trade_status ∈ {TRADE_SUCCESS, TRADE_FINISHED}` + `app_id` 校验，应答 `success` |
| `wechat` | v3 Native `POST /v3/pay/transactions/native` | `Authorization: WECHATPAY2-SHA256-RSA2048`，商户私钥签名 | 平台证书验 `Wechatpay-Signature` + 时间戳窗口 5min + APIv3Key AES-256-GCM 解密 resource，应答 `{"code":"SUCCESS"}` |
| `stripe` | Checkout Session `POST /v1/checkout/sessions` | Bearer SecretKey | `Stripe-Signature` HMAC-SHA256(`t.payload`) + 时间窗 5min + `checkout.session.completed` |
| `hashpay` | `POST {gateway}/api/v1/payment/create` | HMAC-SHA256（ASCII 升序拼接 + secret），可切 MD5 | 同算法验签 + 状态字段白名单 |

> HashPay 无公开统一规范，adapter 内所有字段名、路径、签名算法均可通过渠道配置覆盖（`endpoint_create` / `endpoint_query` / `sign_type` / `field_map`），**接口变更只需改 `internal/payment/hashpay/`**。

---

## 7. 自动发货流程

```
                       ┌── 下单时（预占）──────────────────────────┐
创建订单 ─▶ 事务开始
             ├─ 校验商品上架 / 数量 / 单笔上限
             ├─ auto:   claimCodes(product_id, qty, status unused→locked, order_id=?)
             │           └─ CAS：UPDATE ... WHERE id IN (..) AND status='unused'
             │              RowsAffected 必须 == qty，否则回滚 → PRODUCT_OUT_OF_STOCK
             ├─ manual: UPDATE products SET stock=stock-qty WHERE id=? AND stock>=qty
             │           （stock=-1 跳过）RowsAffected==0 → PRODUCT_OUT_OF_STOCK
             └─ stock_reserved = 1
           事务提交
                       └──────────────────────────────────────────┘

                       ┌── 支付成功（交付）────────────────────────┐
HandlePaymentSuccess ─▶ 事务开始
             ├─ 锁订单行（MySQL FOR UPDATE / SQLite 全局写锁）
             ├─ 幂等：status ∈ {paid, waiting_delivery, completed} → 直接 return，已处理
             ├─ 金额校验：notifyAmount == order.pay_amount，否则 PAYMENT_AMOUNT_MISMATCH
             ├─ 写支付信息 + TransitionTo(paid) + paid_at
             ├─ 核销优惠券（CAS used_count + insert coupon_usages 唯一约束）
             ├─ sales_count += qty
             ├─ auto:   locked→sold（sold_at），写入 order_item.delivery_content
             │           └─ 若 locked 数量不足（迟到回调场景）→ 重新 claim；仍不足 → needs_attention
             │           TransitionTo(completed) + delivered_at
             ├─ manual: TransitionTo(waiting_delivery)
             └─ 写 payment_logs(event=paid)
           事务提交
                    ▼
           异步 goroutine：发送邮件（失败只写 email_logs，绝不影响已提交的支付事务）
                       └──────────────────────────────────────────┘
```

**并发安全的三重保障**

1. **CAS UPDATE**：`WHERE status='unused'` + 检查 `RowsAffected`，两个事务不可能同时把同一行从 `unused` 改走。
2. **事务隔离**：MySQL 走行锁（`FOR UPDATE`），SQLite 由 `database.Tx` 持有进程级写互斥 + `BEGIN IMMEDIATE`（SQLite 本身单写者）。
3. **唯一约束兜底**：`coupon_usages(coupon_id, order_id)` UNIQUE、`orders.order_no` UNIQUE。

多实例部署 SQLite 不被支持（文档明确说明）；需要多实例请用 MySQL，此时行锁生效。

---

## 8. 优惠券计算流程

```
verifyCoupon(code, product_id, quantity, email):
  1  coupon = findByCode(code)                    ✗ → COUPON_INVALID
  2  coupon.status == active                      ✗ → COUPON_DISABLED
  3  now >= start_at (若有)                        ✗ → COUPON_NOT_STARTED
  4  now <= expire_at (若有)                       ✗ → COUPON_EXPIRED
  5  usage_limit==0 || used_count < usage_limit   ✗ → COUPON_USED_UP
  6  scope==all || product_id ∈ coupon_products   ✗ → COUPON_NOT_APPLICABLE
  7  original >= min_amount                       ✗ → COUPON_MIN_AMOUNT
  8  per_user_limit==0 ||
     count(coupon_usages where coupon_id & email) < per_user_limit
                                                  ✗ → COUPON_USER_LIMIT
  9  计算：
       fixed   : d = value
       percent : d = original * (10000 - value) / 10000   // value=9000 → 9折
                 if max_discount > 0 { d = min(d, max_discount) }
       d = clamp(d, 0, original)                  // 折后金额永不为负
 10  return { discount: d, pay: original - d }
```

- **创建订单时只校验+计算，不扣 `used_count`** → 用户狂建未支付订单也消耗不掉优惠券。
- **核销发生在支付成功事务内**：`UPDATE coupons SET used_count=used_count+1 WHERE id=? AND (usage_limit=0 OR used_count<usage_limit)`，同时 `INSERT coupon_usages`（唯一约束保证重复回调只记一次）。
- 若支付时优惠券恰好被抢光：**不失败**（钱已收到），记 `needs_attention` + 日志，订单照常发货。宁可少赚一单优惠额度，也不能扣了钱不发货。

---

## 9. API 路由设计

统一前缀 `/api/v1`。响应体恒为 `{ code, message, data }`，分页 `data = { list, page, page_size, total }`。

### 前台（公开）
```
GET    /config                          商城配置（名称/Logo/公告/时区/维护/是否可下单）
GET    /categories                      分类列表
GET    /products                        商品列表 ?category_id&keyword&sort&page&page_size
GET    /products/:slug                  商品详情（含实时可用库存）
POST   /coupons/verify                  优惠券试算 {code, product_id, quantity, email}
GET    /payments                        可用支付渠道（脱敏，只出 id/name/provider/icon）
POST   /orders                          创建订单  [RateLimit]
GET    /orders/query                    ?order_no=&email=  或  ?token=      [RateLimit]
POST   /orders/:order_no/pay            发起支付 {channel_id}               [RateLimit]
GET    /orders/:order_no/status         轮询支付状态（主动 QueryPayment 兜底）
POST   /orders/:order_no/cancel         取消未付款订单
POST   /payments/notify/:provider/:cid  异步回调（无鉴权，靠验签）
GET    /payments/return/:provider/:cid  同步跳转（仅重定向，不作为支付依据）
GET    /uploads/*                       图片静态
```

### 后台 `/api/v1/admin`（除 login 外均需 JWT）
```
POST   /login                           [RateLimit 5/min]
POST   /logout                          令牌立即失效（token_version +1，全端下线）
GET    /profile      PUT /profile/password

GET    /dashboard                       统计 + 趋势
GET    /dashboard/trend?days=7|30       起点自动裁到系统部署当天

CRUD   /categories        /categories/:id/products
CRUD   /products          POST /products/:id/status  POST /upload
GET    /products/:id/codes              单商品卡密列表 ?status&keyword
POST   /products/:id/codes              批量导入 {content:"多行文本"}
DELETE /products/:id/codes              批量删除未使用 {ids[]} 或 {all_unused:true}
DELETE /codes/:id

GET    /codes                           卡密列表 ?product_id&status&keyword&order_no
POST   /codes                           导入 {product_id, content}
DELETE /codes                           跨商品批量删除未使用 {ids[]}
GET    /codes/stats                     全站各状态数量
GET    /codes/inventory                 每个商品的库存分布（可用数升序）
GET    /codes/export                    按当前筛选导出 CSV（含商品名列）

GET    /orders  /orders/:id             POST /orders/:id/deliver
POST   /orders/:id/remark               POST /orders/:id/refund
POST   /orders/:id/resend-mail

CRUD   /coupons                         GET /coupons/:id (含关联商品)
CRUD   /payments (渠道)                 POST /payments/:id/test
GET    /settings  PUT /settings         POST /settings/mail/test
POST   /turnstile/test                  用已存密钥试校验一次（开启人机验证前自查）
CRUD   /admins
GET    /logs/operations  /logs/payments  /logs/emails
```

### 统一错误码（节选，全表见 `docs/ERRORS.md`）
```
0      成功
40001  参数错误        40101 未登录        40301 无权限
40401  资源不存在      42901 请求过于频繁   50001 服务器内部错误

45001 PRODUCT_NOT_FOUND      45002 PRODUCT_OFF_SHELF     45003 PRODUCT_OUT_OF_STOCK
46001 COUPON_INVALID         46002 COUPON_EXPIRED        46003 COUPON_NOT_STARTED
46004 COUPON_USED_UP         46005 COUPON_NOT_APPLICABLE 46006 COUPON_MIN_AMOUNT
46007 COUPON_USER_LIMIT      46008 COUPON_DISABLED
47001 ORDER_NOT_FOUND        47002 ORDER_EXPIRED         47003 ORDER_STATUS_INVALID
47004 ORDER_ALREADY_PAID     47005 ORDER_NOT_PAID
48001 PAYMENT_CHANNEL_NOT_FOUND   48002 PAYMENT_FAILED
48003 PAYMENT_SIGNATURE_INVALID   48004 PAYMENT_AMOUNT_MISMATCH
48005 PAYMENT_DUPLICATE_NOTIFY    48006 PAYMENT_NOT_SUPPORTED
49001 MAIL_SEND_FAILED       49002 SETTING_INVALID
```

---

## 10. 安全设计

| 威胁 | 防御 |
|---|---|
| **SQL 注入** | 全程 GORM 参数化；排序字段走白名单映射，绝不拼接用户输入 |
| **XSS** | 商品富文本后端 `SanitizeHTML` 白名单过滤；前端不对用户内容用 `v-html`（卡密用 `<pre>` 文本节点） |
| **CSRF** | 后台 JWT 放 `Authorization` 头（非 Cookie）→ 天然免疫；前台无会话态 |
| **IDOR（订单越权）** | 订单查询必须 `order_no + email` 双因子，或 32 位随机 `query_token`；邮箱比对用 `subtle.ConstantTimeCompare`；查询接口限流，无法遍历 |
| **暴力登录** | 登录 IP+用户名双维度限流 5 次/分；失败恒定延时；bcrypt cost 12；`token_version` 支持全端下线 |
| **重放攻击** | 微信/Stripe 校验回调时间戳窗口 5 分钟；支付回调基于订单状态幂等 |
| **支付伪造** | 唯一判定点是 `provider.VerifyNotify()`；**永不**因为请求里带 `status=success` 就认账；回调 URL 携带 `channel_id`，且校验 URL 中 `:provider` 与渠道记录一致，防止用 A 渠道的弱签名冒充 B 渠道 |
| **金额篡改** | 回调金额必须 `== orders.pay_amount`（分级严格相等），不等则拒绝并记 `payment_log(event=amount_mismatch)` |
| **重复回调** | 事务内锁订单行 + 状态判定 + `coupon_usages` 唯一约束，10 次回调也只发一次货 |
| **库存超卖** | CAS UPDATE + RowsAffected 校验 + 事务，见 §7 |
| **卡密重复发放** | `product_codes.status` CAS 流转 `unused→locked→sold`，永不回退到已售 |
| **优惠券刷单** | 核销延后到支付成功；`per_user_limit` 按邮箱；`coupon_usages` 唯一约束 |
| **敏感配置泄漏** | 支付/SMTP 密钥出参统一 `MaskSecret()`（保留前 4 + `****`）；提交时值等于掩码 → 保留旧值；`payment_logs` 落库前过滤敏感 key |
| **文件上传** | 校验真实 MIME（`http.DetectContentType`）+ 扩展名白名单 + 大小上限 + 随机文件名 + 固定目录，用户永不控制服务器路径 |
| **管理员权限** | 所有 `/admin/*` 走 JWT 中间件；关键写操作记 `admin_operation_logs`；不允许禁用/删除最后一个可用管理员 |
| **默认弱口令** | 首次启动强制 `/setup` 初始化；密码强度校验（≥8 位）；拒绝 `admin/admin`、`admin/123456` 等黑名单 |
| **传输安全** | 生产强制 HTTPS（文档 + Nginx 示例）；Cookie 无敏感态 |
| **信息泄露** | 生产模式关闭 Gin debug、错误堆栈不外泄、统一错误信息 |

---

## 附：为什么这样做（关键权衡说明）

1. **为什么下单就预占库存，而不是支付时才扣？**
   支付时才扣会出现"用户付完钱发现没货"。预占的代价是未支付订单会占用库存，用 `order_expire_minutes`（默认 15 分钟）+ 定时释放解决。这是数字商品商城的正确取舍。

2. **为什么 percent 存万分比而不是小数？**
   任何小数都会引入浮点。`9000` 表示 9 折，`amount * 9000 / 10000` 是纯整数运算，结果确定、可测试。

3. **为什么不用支付 SDK？**
   官方 SDK（尤其微信）依赖重、版本变动频繁，且会把业务耦合进 SDK 的 model。这些协议的签名逻辑本身只有几十行 stdlib crypto，手写更可控、更易审计、依赖更少 —— 契合"轻量、低依赖"的目标。

4. **为什么 provider 与 channel 分离？**
   同一个 provider 常需多个实例（易支付主线/备线、Stripe 分地区）。`provider` 是代码里的实现，`payment_channel` 是运营配置的一条线路。回调 URL 带 `channel_id` 才能定位到正确的密钥去验签。

5. **为什么迁移分 sqlite/mysql 两套 SQL？**
   `AUTOINCREMENT` vs `AUTO_INCREMENT`、`DATETIME` 精度、索引长度限制等差异无法用一套 DDL 覆盖。与其在业务层写 `if driver ==`，不如把差异**全部封死在 migrations 目录和 database 层**，业务代码彻底无感。
