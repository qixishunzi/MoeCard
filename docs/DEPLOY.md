# 部署

从零把 MoeCard 跑起来，以及上生产前要处理的事。

> 从 [README](../README.md) 拆出来的详细说明。


---

## SQLite 部署

默认模式，**不需要安装任何数据库**。

```bash
DB_DRIVER=sqlite
SQLITE_PATH=/app/data/moecard.db
```

Docker 部署时 `/app/data` 与 `/app/storage` 已配置为 volume，容器重建不会丢数据。

裸机部署记得备份这两个目录：

```bash
# 热备份（WAL 模式下安全）
sqlite3 data/moecard.db ".backup 'backup-$(date +%F).db'"
tar czf storage-$(date +%F).tar.gz storage/
```

> ⚠️ **SQLite 只支持单进程写**。需要多实例 / 多副本部署时请改用 MySQL。
> 系统内部已用进程级写互斥把写事务串行化，单实例下并发完全安全（见并发测试）。


---

## MySQL 部署

```bash
DB_DRIVER=mysql
MYSQL_HOST=mysql
MYSQL_PORT=3306
MYSQL_USERNAME=moecard
MYSQL_PASSWORD=<强密码>
MYSQL_DATABASE=moecard
```

**使用外部 MySQL**：直接改上面的连接信息即可。要求 MySQL 5.7+ / 8.0，
字符集 `utf8mb4`，并且 **`time_zone` 设为 `+00:00`** —— 系统统一以 UTC 存储时间。

```sql
CREATE DATABASE moecard CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'moecard'@'%' IDENTIFIED BY '<强密码>';
GRANT ALL PRIVILEGES ON moecard.* TO 'moecard'@'%';
FLUSH PRIVILEGES;
```

**使用内置 MySQL**：

```bash
# .env 里补上 MYSQL_ROOT_PASSWORD 和 MYSQL_PASSWORD
docker compose --profile mysql up -d
```

---


---

## Docker 部署

```bash
docker compose up -d          # 启动
docker compose logs -f        # 看日志
docker compose down           # 停止（数据保留在 volume 中）
docker compose up -d --build  # 更新后重新构建
```

### 配 Nginx 反向代理（生产必备）

```nginx
server {
    listen 443 ssl http2;
    server_name shop.example.com;

    ssl_certificate     /etc/letsencrypt/live/shop.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/shop.example.com/privkey.pem;

    client_max_body_size 10M;   # 图片上传

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 60s;
    }
}

server {
    listen 80;
    server_name shop.example.com;
    return 301 https://$host$request_uri;
}
```

用了反向代理后，把 `.env` 里的 `TRUST_PROXY` 设为 `true`，
否则限流会把所有请求都当成来自同一个 IP（代理的 IP）。

> ⚠️ **只有在可信代理后面才设 `TRUST_PROXY=true`**。
> 直接暴露到公网时开启它，任何人都能伪造 `X-Forwarded-For` 绕过限流。

---


---

## 开机自启

```bash
sudo ./moecard -install-service
```

它做这几件事，都是手写单元文件时最容易漏的：

1. 建一个 `moecard` 系统用户（`--system`、nologin、不建家目录）
2. 把安装目录 chown 给它
3. 按**当前实际路径**生成 `/etc/systemd/system/moecard.service` ——
   不是写死的 `/opt/moecard`，装在哪就填哪
4. `daemon-reload` + `enable` + `start`

装完：

```bash
systemctl status moecard      # 状态
journalctl -u moecard -f      # 日志
systemctl restart moecard     # 重启
```

取消：

```bash
sudo ./moecard -uninstall-service
```

只停服务、禁用、删单元文件。**数据、配置和 `moecard` 用户都保留**，
要删用户它会把命令告诉你。

### 想自己改单元文件

```bash
./moecard -print-service > moecard.service   # 打印出来自己改
```

`scripts/moecard.service` 里那份是同一份内容（由这条命令生成，有测试钉住
两边不会漂），改完手动拷到 `/etc/systemd/system/` 再 `daemon-reload`。

单元里带了一组沙箱限制（`ProtectSystem=full`、`NoNewPrivileges`、限制
地址族等），是「万一被 RCE 了对方能干什么」的边界。**注意 `.env` 里的
`SQLITE_PATH` 和 `STORAGE_LOCAL_PATH` 必须是相对路径** —— 写成绝对路径的话
`ReadWritePaths` 挡不住它，服务会因为写不了那个目录起不来。

### 其它系统

`-install-service` 只支持 Linux 的 systemd。macOS 用 launchd、Windows 用
任务计划程序或 nssm，命令会明确告诉你，不会装一半。

---

## 升级

```bash
cd /opt/moecard
sudo sh scripts/upgrade.sh
```

它按这个顺序走，任何一步失败都会回滚：

1. 查有没有新版本，已是最新就直接退出
2. 用一致性快照备份数据库（`backup-before-upgrade-*.db`）
3. **二次确认**，明确告诉你站点会短暂无法访问
4. 停服务 → `moecard -update`（校验 SHA256）→ 起服务
5. 验活最多等 15 秒，起不来就把 `moecard.old` 换回去并重新拉起服务

只想换二进制、服务自己管：

```bash
./moecard -check-update    # 只查
./moecard -update          # 查 + 确认 + 装，不碰服务
./moecard -update -y       # 跳过确认，给无人值守脚本用
```

`upgrade.sh` 在非交互环境（管道、cron、CI）下会直接拒绝 —— 升级要停服务、
换可执行文件，在没人看着的时候悄悄做完比不升级危险得多。真要无人值守就用
上面的 `-update -y`，那是显式表态。

---

## 卸载

```bash
cd /opt/moecard
sudo sh scripts/uninstall.sh
```

确认分两层，因为两件事的可逆性完全不同：

| 操作 | 确认方式 | 可逆 |
|---|---|---|
| 停止并移除 systemd 服务 | `y/N` | 是，重新 `-install-service` 就行 |
| 删除安装目录（数据、卡密、密钥） | **手打完整路径** | 否 |

为什么删数据不能只按 y：前一个问题刚敲完 y，手还没离开键盘，
下一个 y 就下去了。逼你把 `/opt/moecard` 打一遍，至少能确认删的是哪个目录。

删之前会先问要不要备份数据库，选 y 会存到你的家目录。

只想撤掉开机自启、留着数据：走到第二层确认时 Ctrl-C 就行，服务那时已经移除了。
或者直接用 `sudo ./moecard -uninstall-service`，它压根不碰数据。

---

## 环境变量

完整列表见 [`.env.example`](../.env.example)。**必须配置**的只有三项：

| 变量 | 说明 |
|---|---|
| `JWT_SECRET` | 管理员 token 签名密钥，**生产环境必须 ≥ 32 位**。`openssl rand -hex 32` |
| `BASE_URL` | 后端公网地址。**支付回调会打到这里，填错就收不到支付通知** |
| `FRONTEND_URL` | 前端地址。用于支付跳转与邮件里的订单链接 |

其余（商城名称、SMTP、支付密钥等）**都在后台界面配置**，不用改文件、不用重启。

---


---

## 数据库迁移

迁移 SQL 已 embed 进二进制，**服务启动时自动执行**，无需手动操作。

手动执行：

```bash
# 查看状态
docker compose exec moecard /app/moecard-migrate -status

# 执行未应用的迁移
docker compose exec moecard /app/moecard-migrate

# 本地
cd server && go run ./cmd/migrate -status
```

跳过启动时自动迁移（灰度发布场景）：

```bash
/app/moecard -skip-migrate
```

### 新增迁移

在 `server/migrations/sqlite/` 与 `server/migrations/mysql/` 下**各加一个同名文件**：

```
0002_add_something.sql
```

按文件名升序执行，每个文件在单个事务内完成（失败整体回滚），
已执行的版本记录在 `schema_migrations` 表中。

> 为什么两套 SQL：`AUTOINCREMENT` vs `AUTO_INCREMENT`、索引长度限制、
> 列类型等差异无法用一套 DDL 覆盖。与其在业务层写 `if driver ==`，
> 不如把差异**全部封死在 migrations 目录里**。

---


---

## 生产环境

上线前请逐项确认：

- [ ] `APP_ENV=production`
- [ ] `JWT_SECRET` 已用 `openssl rand -hex 32` 生成（≥ 32 位）
- [ ] `BASE_URL` / `FRONTEND_URL` 是**公网可访问的 HTTPS 地址**
- [ ] 已配置 Nginx + HTTPS，并设置 `TRUST_PROXY=true`
- [ ] `.env` 权限设为 `600`，且**没有**被提交到 Git
- [ ] 管理员密码足够强（不是弱口令）
- [ ] SMTP 已配置并发送过测试邮件
- [ ] 每个支付渠道的回调地址**已填到支付平台后台**并点过「测试」
- [ ] 已用**小额真实订单**跑通一次完整流程：下单 → 支付 → 回调 → 发货 → 收到邮件
- [ ] `/app/data` 与 `/app/storage` 已配置定期备份
- [ ] MySQL 模式下 `time_zone` 为 `+00:00`

### 上线后的验证（强烈建议）

```bash
# 1. 故意破坏签名，确认回调被拒绝
#    → 后台「系统日志 → 支付日志」应出现 notify_invalid

# 2. 让支付平台重复推送同一条回调
#    → 订单只发货一次，日志中出现 duplicate

# 3. 篡改回调金额
#    → 被 PAYMENT_AMOUNT_MISMATCH 拦截
```

### 备份

```bash
# SQLite
docker compose exec moecard sqlite3 /app/data/moecard.db ".backup '/app/data/backup.db'"
docker cp moecard:/app/data/backup.db ./backup-$(date +%F).db

# MySQL
docker compose exec mysql mysqldump -u root -p"$MYSQL_ROOT_PASSWORD" \
  --single-transaction --routines moecard > backup-$(date +%F).sql

# 上传文件
docker run --rm -v moecard_moecard-storage:/data -v $(pwd):/backup alpine \
  tar czf /backup/storage-$(date +%F).tar.gz -C /data .
```

---
