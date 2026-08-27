<div align="center">

<img src="web/public/icon-192.png" width="112" alt="MoeCard" />

# MoeCard

**数字商品自动发货商城** · 卡密 / 软件授权 / 会员账号 / 游戏点卡

下载一个二进制就能跑。前端已嵌入，SQLite 零外部依赖。

[![CI](https://github.com/qixishunzi/MoeCard/actions/workflows/ci.yml/badge.svg)](https://github.com/qixishunzi/MoeCard/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/qixishunzi/MoeCard?label=release)](https://github.com/qixishunzi/MoeCard/releases/latest)
[![Downloads](https://img.shields.io/github/downloads/qixishunzi/MoeCard/total)](https://github.com/qixishunzi/MoeCard/releases)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

[Telegram 频道](https://t.me/moecard) · [交流群](https://t.me/moecard_group)

</div>

---

## 安装

**Linux / macOS**

```bash
curl -fsSL https://raw.githubusercontent.com/qixishunzi/MoeCard/main/scripts/install.sh | sh
cd moecard
cp .env.example .env
./moecard
```

**Windows**

```powershell
irm https://raw.githubusercontent.com/qixishunzi/MoeCard/main/scripts/install.ps1 | iex
```

**Docker**

```bash
git clone https://github.com/qixishunzi/MoeCard.git && cd MoeCard
cp .env.example .env
docker compose up -d
```

装好后打开 `http://你的地址:8080`，会自动跳到初始化页面创建管理员。

**开机自启**（Linux）

```bash
sudo ./moecard -install-service
```

自动建专用用户、设权限、写 systemd 单元、启用并启动。取消用
`sudo ./moecard -uninstall-service`（只撤服务，数据不动）。

也可以到 [Releases](https://github.com/qixishunzi/MoeCard/releases/latest) 手动下载，
支持 Linux / Windows / macOS × x86_64 / ARM64 六个组合。

---

## 上线前必做

`.env` 里至少改这三项，其余保持默认即可：

```bash
JWT_SECRET=换成随机值        # openssl rand -hex 32
BASE_URL=https://你的域名     # 支付回调要打到这里
FRONTEND_URL=https://你的域名
```

然后在后台完成三件事，否则会出问题：

1. **邮件配置** → 配好 SMTP 并发一封测试邮件，不然买家收不到卡密
2. **支付方式** → 加一个渠道并点「测试」
3. **管理员** → 开启两步验证

用小额真实订单跑通一次「下单 → 支付 → 回调 → 发货 → 收到邮件」再正式开张。

更多（数据库加密、CDN 真实 IP、反向代理、systemd）见 [部署文档](docs/DEPLOY.md)。

---

## 功能

**交易**

- 自动发货 —— 支付成功自动分配卡密、邮件送达，并发安全，绝不重复发卡
- 手动发货 —— 支付后转「待发货」，可要求买家下单时填账号、大区等信息
- 支付渠道 —— 易支付 V1 / V2、支付宝官方、微信支付 V3、Stripe、HashPay
- 优惠券 —— 满减 + 折扣，可限商品、限总次数、限单人次数
- 退款 —— 渠道自动退款与人工退款记账，都完整留痕

**商城前台**

- 首页轮播图（可绑定商品）、公告弹窗、边打边搜
- 客服联系方式：Telegram / WhatsApp 点击跳转，微信 / QQ / 邮箱点击复制
- 免登录下单，凭订单号 + 邮箱查询和查看卡密

**后台**

- 卡密管理独立成页，批量导入去重，明文查看留审计
- 商家通知 —— Telegram / Bark / 企业微信 / Webhook，待发货和缺货推到手机
- 两步验证、自定义后台入口、Cloudflare 人机验证
- 订单与卡密 CSV 导出，一致性快照备份

**安全**

- 金额全程整数分运算，不碰浮点
- 支付回调验签 + 金额校验 + 幂等，平台重复推十次也只发一次货
- 卡密与 TOTP 密钥 AES-256-GCM 加密落库
- 密钥在接口里一律脱敏，提交脱敏值不会覆盖已存的真实值
- 客户端 IP 内置 Cloudflare / 腾讯云 EdgeOne / 阿里云 ESA 等回源头部识别

---

## 升级与卸载

```bash
sudo sh scripts/upgrade.sh     # 升级：停服务 → 更新 → 起服务 → 验活
sudo sh scripts/uninstall.sh   # 卸载
```

两个都会二次确认，非交互环境（管道、cron）一律拒绝执行。

升级会先备份数据库，任何一步失败自动回滚到旧版本并把服务拉起来。
只想换二进制不管服务的话用 `./moecard -update`（`-check-update` 只查不装）。

卸载分两层：撤服务按 y 就行，**删数据要手打安装目录的完整路径** ——
里面是订单记录和没卖出去的卡密，y/N 挡不住手快。

Docker 用户走镜像：`docker compose pull && docker compose up -d`

---

## 文档

| | |
|---|---|
| [部署](docs/DEPLOY.md) | SQLite / MySQL / Docker、环境变量、生产环境、数据库迁移 |
| [支付配置](docs/PAYMENT.md) | 六种渠道怎么申请、怎么填、怎么排错 |
| [后台配置](docs/CONFIG.md) | SMTP、商家通知、静态加密、两步验证、备份导出 |
| [常见问题](docs/FAQ.md) | 订单卡住、收不到邮件、忘记密码…… |
| [开发](docs/DEVELOP.md) | 技术栈、目录结构、本地开发、API 文档 |
| [架构](docs/ARCHITECTURE.md) | 分层、并发与幂等的实现细节 |

---

## 参与

有问题或想法欢迎 [提 Issue](https://github.com/qixishunzi/MoeCard/issues)，
或者到 [Telegram 群](https://t.me/moecard_group) 说。

改代码前先跑一遍 `cd server && go test ./...` 和 `cd web && npm run build`，
CI 会卡住格式化、`go vet` 和这两项。

---

## 许可

MIT。作者 [Qixishunzi](https://github.com/qixishunzi)。
