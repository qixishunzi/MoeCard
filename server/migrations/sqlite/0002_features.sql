-- 0002: 通知 / 库存告警 / 买家自定义字段 / 卡密加密 / 两步验证
--
-- SQLite 的 ALTER TABLE ADD COLUMN 是元数据操作，不重写表，对大表也很快。

-- ---- 商品：库存告警阈值 + 买家自定义字段定义 ----
-- low_stock_threshold = 0 表示沿用全局默认值（设置项 low_stock_threshold）
-- low_stock_notified_at 用于抑制重复告警：补货后清空，再次跌破阈值才会重新提醒
ALTER TABLE products ADD COLUMN low_stock_threshold   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE products ADD COLUMN low_stock_notified_at DATETIME;
-- custom_fields 是 JSON 数组，描述下单页要额外收集哪些信息（代充类商品需要买家账号）
ALTER TABLE products ADD COLUMN custom_fields         TEXT    NOT NULL DEFAULT '';

-- ---- 订单：买家填写的自定义信息 ----
-- custom_data 是 JSON 对象（字段 key -> 买家填写的值）
ALTER TABLE orders ADD COLUMN custom_data TEXT NOT NULL DEFAULT '';

-- ---- 管理员：TOTP 两步验证 ----
-- totp_secret 落库前用主密钥加密；totp_recovery 存的是恢复码的哈希，不是明文
ALTER TABLE admins ADD COLUMN totp_secret   TEXT    NOT NULL DEFAULT '';
ALTER TABLE admins ADD COLUMN totp_enabled  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE admins ADD COLUMN totp_recovery TEXT    NOT NULL DEFAULT '';

-- ---- 卡密：静态加密 ----
-- 加密后是 "enc:v1:<base64>" 形态，比明文长约 40%。
-- SQLite 的 TEXT 无长度上限，这里不需要改类型，加一列标记便于排查与灰度。
ALTER TABLE product_codes ADD COLUMN encrypted INTEGER NOT NULL DEFAULT 0;

-- ---- 通知发送记录 ----
-- 与邮件日志同样的定位：通知失败绝不能影响主业务，这里是唯一的失败留痕
CREATE TABLE notify_logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    channel    TEXT    NOT NULL,               -- telegram | bark | wecom | webhook
    event      TEXT    NOT NULL,               -- order_paid | manual_delivery | needs_attention | low_stock | refund
    title      TEXT    NOT NULL DEFAULT '',
    content    TEXT    NOT NULL DEFAULT '',
    status     TEXT    NOT NULL,               -- success | failed
    error      TEXT    NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL
);
CREATE INDEX idx_notify_logs_created ON notify_logs (created_at);
CREATE INDEX idx_notify_logs_event   ON notify_logs (event, created_at);
