MoeCard 是一套面向数字商品（卡密、软件授权、会员账号、游戏点卡）的自动发货商城。
Go + Vue 3，前端已嵌进可执行文件，**下载一个二进制就能跑**，SQLite 零外部依赖。

## 装

Linux / macOS：

```bash
curl -fsSL https://raw.githubusercontent.com/qixishunzi/MoeCard/main/scripts/install.sh | sh
cd moecard && cp .env.example .env    # 改掉 JWT_SECRET，填好域名
./moecard
```

Windows：

```powershell
irm https://raw.githubusercontent.com/qixishunzi/MoeCard/main/scripts/install.ps1 | iex
```

也可以直接在下面挑对应平台的包。装完浏览器打开 `http://你的地址:8080` 建管理员账号。

| 系统 | 架构 | 文件 |
|---|---|---|
| Linux | x86_64 / ARM64 | `moecard_1.0.0_linux_amd64.tar.gz` / `_arm64.tar.gz` |
| Windows | x86_64 / ARM64 | `moecard_1.0.0_windows_amd64.zip` / `_arm64.zip` |
| macOS | Intel / Apple Silicon | `moecard_1.0.0_darwin_amd64.tar.gz` / `_arm64.tar.gz` |

下载后建议校验：

```bash
sha256sum -c SHA256SUMS.txt --ignore-missing
```

## 这个版本有什么

**交易**

- 自动发货：支付成功后自动分配卡密、发邮件送达。并发安全，绝不重复发卡
- 手动发货：支付后转「待发货」，后台填写内容后完成并通知买家
- 支付渠道：易支付 V1 / V2、支付宝官方、微信支付 V3、Stripe、HashPay
- 优惠券：满减 + 百分比折扣，可限定商品、总次数、单人次数
- 退款：渠道自动退款与人工退款记账，都完整留痕
- 库存：自动发货按未使用卡密数实时计算；手动发货可设 `-1` 无限库存

**商城前台**

- 首页轮播图，可绑定商品，点图直达
- 公告弹窗，可设强制阅读倒计时
- 边打边搜，清空即回到全部商品
- 客服联系方式（Telegram / WhatsApp / 微信 / QQ / 邮箱），前两者点击跳转，其余点击复制
- 下单时可要求买家填写额外信息（游戏账号、大区等），后台发货弹窗直接显示并可一键复制

**后台**

- 卡密管理独立成页，商品是它的一个筛选项
- 商家通知：Telegram / Bark / 企业微信 / Webhook，待发货订单与库存告急直接推到手机
- 两步验证（TOTP + 一次性恢复码）
- 自定义后台入口路径，替掉默认的 `/admin`
- Cloudflare Turnstile 人机验证，可按场景开关
- 订单与卡密 CSV 导出，一致性快照备份

**安全**

- 金额全程整数分运算，不碰浮点
- 支付回调验签、金额校验、幂等：重复推送十次也只发一次货
- 卡密与 TOTP 密钥 AES-256-GCM 静态加密落库
- 密钥在接口里一律脱敏，提交脱敏值不会覆盖已存的真实值
- 客户端 IP 识别内置 Cloudflare、腾讯云 EdgeOne、阿里云 ESA / CDN、Akamai、Nginx
  的回源头部，也可自定义；只在显式开启 `TRUST_PROXY` 后才信任请求头

## 升级与卸载

包里带了两个脚本，都会二次确认，非交互环境（管道、cron）一律拒绝执行：

```bash
sudo sh scripts/upgrade.sh     # 停服务 → 更新 → 起服务 → 验活，失败自动回滚
sudo sh scripts/uninstall.sh   # 卸载，删数据要手打完整路径才算数
```

开机自启一条命令：

```bash
sudo ./moecard -install-service
```

只想换二进制不管服务：

```bash
./moecard -check-update    # 只看
./moecard -update          # 检查、确认、安装
```

会比对同一个 Release 里的 `SHA256SUMS.txt`，**校验不通过就中止**，没有「校验文件缺失
就跳过校验」这条退路。旧版本备份成 `moecard.old`，替换失败自动回滚。更新后需要重启服务。

后台「关于」页也有「检查更新」，但只查不装——换掉可执行文件必须重启进程，那该由
systemd / Docker 或你自己来做。

Docker 用户走镜像：`docker compose pull && docker compose up -d`

> 自更新保护的是传输篡改和下载损坏；**不**保护 GitHub 账号被攻陷或发布者主动发恶意
> 版本——校验和与二进制来自同一个 Release。要挡住那种情况需要离线私钥签名，代价是
> 密钥管理，暂时没上。

## 上线前请务必

1. `.env` 里的 `JWT_SECRET` 换成随机值：`openssl rand -hex 32`
2. 配好 SMTP 并发一封测试邮件，否则买家收不到卡密
3. 加一个支付渠道并点「测试」
4. 加 `DATA_ENCRYPTION_KEY` 开启卡密静态加密，密钥与数据库分开保管
5. 后台开启两步验证
6. 套了 CDN 或反向代理的话，`.env` 加 `TRUST_PROXY=true`，然后到
   后台 → 商城设置 → 安全 → 客户端 IP 识别，确认识别到的是你的真实 IP
7. 用小额真实订单跑通一次：下单 → 支付 → 回调 → 发货 → 收到邮件

完整文档见 [README](https://github.com/qixishunzi/MoeCard#readme)。
问题反馈 [Issues](https://github.com/qixishunzi/MoeCard/issues)，
交流 [Telegram 频道](https://t.me/moecard) · [群组](https://t.me/moecard_group)。
