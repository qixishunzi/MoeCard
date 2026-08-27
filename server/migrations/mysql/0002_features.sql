-- 0002: 通知 / 库存告警 / 买家自定义字段 / 卡密加密 / 两步验证

-- ---- 商品：库存告警阈值 + 买家自定义字段定义 ----
-- low_stock_threshold = 0 表示沿用全局默认值（设置项 low_stock_threshold）
-- low_stock_notified_at 用于抑制重复告警：补货后清空，再次跌破阈值才会重新提醒
ALTER TABLE `products` ADD COLUMN `low_stock_threshold`   INT      NOT NULL DEFAULT 0 COMMENT '低库存告警阈值，0=用全局默认';
ALTER TABLE `products` ADD COLUMN `low_stock_notified_at` DATETIME NULL COMMENT '上次低库存告警时间，补货后清空';
-- custom_fields 是 JSON 数组，描述下单页要额外收集哪些信息（代充类商品需要买家账号）
ALTER TABLE `products` ADD COLUMN `custom_fields`         TEXT     NULL COMMENT '买家自定义字段定义(JSON)';

-- ---- 订单：买家填写的自定义信息 ----
ALTER TABLE `orders` ADD COLUMN `custom_data` TEXT NULL COMMENT '买家填写的自定义信息(JSON)';

-- ---- 管理员：TOTP 两步验证 ----
-- totp_secret 落库前用主密钥加密；totp_recovery 存的是恢复码的哈希，不是明文
ALTER TABLE `admins` ADD COLUMN `totp_secret`   VARCHAR(255) NOT NULL DEFAULT '' COMMENT 'TOTP 密钥(加密存储)';
ALTER TABLE `admins` ADD COLUMN `totp_enabled`  TINYINT(1)   NOT NULL DEFAULT 0;
ALTER TABLE `admins` ADD COLUMN `totp_recovery` TEXT         NULL COMMENT '恢复码哈希列表(JSON)';

-- ---- 卡密：静态加密 ----
-- 密文是 "enc:v1:<base64>" 形态，比明文长约 40%，
-- 原来的 VARCHAR(1000) 装不下，必须放宽
ALTER TABLE `product_codes` MODIFY COLUMN `content` VARCHAR(2000) NOT NULL;
ALTER TABLE `product_codes` ADD COLUMN `encrypted` TINYINT(1) NOT NULL DEFAULT 0;

-- ---- 通知发送记录 ----
CREATE TABLE `notify_logs` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `channel`    VARCHAR(24)  NOT NULL COMMENT 'telegram | bark | wecom | webhook',
    `event`      VARCHAR(32)  NOT NULL COMMENT 'order_paid | manual_delivery | needs_attention | low_stock | refund',
    `title`      VARCHAR(255) NOT NULL DEFAULT '',
    `content`    TEXT         NULL,
    `status`     VARCHAR(16)  NOT NULL COMMENT 'success | failed',
    `error`      VARCHAR(1000) NOT NULL DEFAULT '',
    `created_at` DATETIME     NOT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_notify_logs_created` (`created_at`),
    KEY `idx_notify_logs_event` (`event`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='通知发送记录';
