# 开发

改代码之前先看这个。架构细节另见 ARCHITECTURE.md。

> 从 [README](../README.md) 拆出来的详细说明。


---

## 技术栈

**后端** Go 1.24 · Gin · GORM · SQLite(纯 Go) / MySQL · JWT · log/slog

**前端** Vue 3 · TypeScript · Vite · Vue Router · Pinia · Axios · Tailwind CSS 4 · lucide

### 三个关键取舍

**1. 不使用任何支付 SDK**

支付宝、微信、Stripe 的官方 SDK 依赖重、版本变动频繁，还会把业务耦合进 SDK 的 model。
这些协议的密码学部分本身只有几十行标准库代码（RSA-SHA256 签名、AES-GCM 解密、HMAC）。
手写更可控、更易审计、依赖更少。全部实现在 [`internal/payment/crypto.go`](../server/internal/payment/crypto.go)。

**2. SQLite 用纯 Go 驱动**

`glebarez/sqlite`（基于 modernc）免 CGO，因此可以 `CGO_ENABLED=0` 静态编译，
产物在 Alpine 容器里直接跑，也能交叉编译到任意平台。

**完整依赖只有 8 个**：gin、gorm、gorm/driver/mysql、glebarez/sqlite、golang-jwt、
golang.org/x/crypto、golang.org/x/net、godotenv、yaml.v3。

TOTP（RFC 6238）、AES-GCM 静态加密、四种通知渠道也都是标准库手写，
和支付协议同一个判断：算法本身只有几十行，手写更可控、可审计、依赖更少。

**3. 前端不使用组件库**

商城前台与管理后台是两套完全不同的视觉语言（见下），硬套一个组件库
做不出想要的效果，而且会一直和它的默认样式打架。改用 Tailwind + 手写的
`Wd*` / `Jp*` 组件，顺带少掉一个 800KB JS + 240KB CSS 的依赖 ——
现在整个前端压缩后不到 100KB。

---


---

## 目录结构

```
MoeCard/
├── docs/
│   ├── ARCHITECTURE.md      架构设计（ER 图、状态机、支付架构、安全设计）
│   ├── ERRORS.md            错误码表
│   └── ...
├── server/
│   ├── cmd/
│   │   ├── server/          API 服务入口
│   │   └── migrate/         独立迁移工具
│   ├── internal/
│   │   ├── api/             统一 Response + 错误码
│   │   ├── router/          路由注册 + OpenAPI 文档
│   │   ├── handler/         HTTP 层（public / admin）
│   │   ├── middleware/      JWT / 限流 / 日志 / CORS / 维护模式
│   │   ├── service/         业务逻辑（事务边界都在这一层）
│   │   ├── repository/      数据访问
│   │   ├── model/           实体 + 订单状态机
│   │   ├── payment/         PaymentProvider 抽象 + 6 个 Adapter
│   │   ├── mail/            SMTP + 邮件模板
│   │   ├── storage/         文件存储抽象 + 本地实现
│   │   ├── database/        连接 / 事务 / 驱动差异封装 / 迁移执行
│   │   ├── config/ logger/ utils/
│   │   └── web/dist/        前端构建产物（embed 进二进制）
│   └── migrations/{sqlite,mysql}/
├── web/                     Vue 工程（前台 + 后台同一项目）
├── Dockerfile
├── docker-compose.yml
└── .env.example
```

**分层红线**（代码评审时重点看这几条）

- `handler` 只做「绑参数 → 调 service → 写响应」，不出现 SQL 或业务分支
- `service` 是唯一的事务边界，不出现 `gin.Context`
- `repository` 只做数据访问，不开事务、不调其他 service
- `payment/*` 只做「通信 + 签名 + 验签 + 解析」，**绝不发货、绝不改订单**
- 数据库方言差异只允许出现在 `internal/database` 与 `migrations/` 里

---


---

## 开发环境

```bash
# 后端
cd server
go run ./cmd/server
go test ./...              # 全部测试
go test ./internal/service/ -v -run TestConcurrent   # 并发测试
gofmt -l . && go vet ./...

# 前端
cd web
npm run dev                # 热更新，:5173
npm run type-check         # 类型检查
npm run build              # 构建（含类型检查）
```

**建议在 CI 里加上竞态检测**（需要 gcc / CGO）：

```bash
CGO_ENABLED=1 go test ./... -race -count=1
```

### 测试覆盖的关键场景

| 测试 | 验证内容 |
|---|---|
| `TestConcurrentOrders_NoDuplicateCodes` | **100 并发下单抢 30 张卡密** → 恰好 30 单成功，无一张卡密被重复分配 |
| `TestConcurrentOrders_MultiQuantity` | 一单多张时的并发分配正确性 |
| `TestConcurrentOrders_ManualStock` | 手动发货商品并发下单不超卖 |
| `TestConcurrentPaymentNotify_Idempotent` | **同一订单并发推 20 次支付通知** → 只发一次货、只核销一次券、销量只加一次 |
| `TestPaymentAmountMismatch` | 金额被篡改的回调被拒绝，且不发货 |
| `TestLatePaymentAfterExpiry` | 订单过期后才到的支付回调 → 复活订单；无货时标记人工处理而非丢单 |
| `TestOrderExpiry_ReleasesStock` | 订单过期释放卡密与库存 |
| `TestOrderStatusMachine` | 12 条合法转换全通过，13 条非法转换全拒绝 |
| `TestOrderQueryIDOR` | 错误邮箱 / 错误 token 无法查看他人订单 |
| `TestCouponValidation` | 优惠券 9 个失败分支逐一覆盖 |
| `TestCouponNotConsumedOnOrderCreate` | 建 10 个未支付订单不消耗券额度（防刷） |
| `TestOrderSnapshot` | 商品改名/改价/删除后历史订单信息不变 |
| `payment/yipayv1`、`payment/stripe` | 伪造签名、篡改金额、冒充商户号、重放攻击全部拒绝 |

---


---

## API 文档

服务启动后访问 **`/api/docs`** 查看交互式 API 文档（Swagger UI）。

OpenAPI 3.0 规范文件：`/api/docs/openapi.yaml`
（源文件 [`server/internal/router/openapi.yaml`](../server/internal/router/openapi.yaml)，手写维护，随代码一起 review）

**统一响应结构**

```json
{ "code": 0, "message": "success", "data": {} }
```

分页：`data = { list, page, page_size, total }`

错误码全表见 [`docs/ERRORS.md`](ERRORS.md)。
**前端必须依赖数字 `code` 判断，不要依赖 `message` 字符串。**

**金额**：所有金额字段都是 `int64` 最小货币单位（人民币 = 分）。`1000` 表示 ¥10.00。

---


---

## 视觉语言

前台与后台共用同一套设计系统 —— **暖色仪表盘（Warm Dashboard）**，
照着 `模板/管理员` 的规范手写：

| Token | 值 |
|---|---|
| 页面底色 | `#faf8f5` |
| 卡片 | `bg-white rounded-2xl shadow-xl shadow-black/[0.04]` |
| 主色 | 青 `#4a9d9a` |
| 辅助色 | 琥珀 `#e8b86d` · 陶土 `#c17767` · 灰青 `#6b8e8e` |
| 字重 | `font-medium` / `font-semibold`（正文 400） |
| 过渡 | `duration-200`，悬停抬升 `-translate-y-0.5` |
| 圆角 | 卡片 16px · 按钮与输入框 12px · 徽章 8px |

组件全部在 [`src/ui`](../web/src/ui)（`Wd*` 前缀），前台与后台直接复用同一套 ——
按钮、输入框、卡片、表格、弹窗、Toast、确认框都只有一份实现。

正文与信息性文字的对比度均 ≥ WCAG AA 4.5:1。两处例外是模板本身的配色：
白字青底按钮与青色小字徽章为 3.19:1（大字号下达到 AA Large 3:1）——
为与模板保持一致而保留。全站带 `prefers-reduced-motion` 降级。

---


---

## 后续可扩展方向

架构上已为这些功能预留：

- **购物车 / 一单多商品** —— `orders` / `order_items` 已是一对多结构
- **用户注册登录、会员、余额、积分** —— 订单以邮箱为主键，加用户表后可平滑关联
- **对象存储（S3/OSS/COS/R2）** —— 实现 `storage.Provider` 接口即可
- **更多支付渠道** —— 实现 `payment.Provider` 接口 + 注册一行
- **Telegram / Webhook 通知** —— 在 `HandlePaymentSuccess` 提交事务后挂钩
- **API 下单** —— 复用现有 service，加一层 API Key 鉴权

---
