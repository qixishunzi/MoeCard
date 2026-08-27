-- MoeCard 初始化 schema (MySQL 5.7+ / 8.0)
-- 约定：
--   * 金额一律 BIGINT，单位 = 最小货币单位（人民币 = 分）
--   * 时间一律 DATETIME，存 UTC（DSN 必须带 parseTime=True&loc=UTC）
--   * 字符集 utf8mb4，索引字段长度 <= 191 以兼容 InnoDB 767 字节限制

CREATE TABLE `admins` (
    `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `username`      VARCHAR(64)  NOT NULL,
    `password_hash` VARCHAR(255) NOT NULL,
    `nickname`      VARCHAR(64)  NOT NULL DEFAULT '',
    `status`        VARCHAR(16)  NOT NULL DEFAULT 'active',
    `token_version` INT          NOT NULL DEFAULT 1,
    `last_login_at` DATETIME     NULL,
    `last_login_ip` VARCHAR(64)  NOT NULL DEFAULT '',
    `created_at`    DATETIME     NOT NULL,
    `updated_at`    DATETIME     NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_admins_username` (`username`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE `categories` (
    `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `name`        VARCHAR(64)  NOT NULL,
    `slug`        VARCHAR(100) NOT NULL,
    `description` VARCHAR(500) NOT NULL DEFAULT '',
    `icon`        VARCHAR(255) NOT NULL DEFAULT '',
    `sort`        INT          NOT NULL DEFAULT 0,
    `status`      VARCHAR(16)  NOT NULL DEFAULT 'active',
    `created_at`  DATETIME     NOT NULL,
    `updated_at`  DATETIME     NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_categories_slug` (`slug`),
    KEY `idx_categories_status_sort` (`status`, `sort`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE `products` (
    `id`             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `category_id`    BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `name`           VARCHAR(191) NOT NULL,
    `slug`           VARCHAR(150) NOT NULL,
    `cover`          VARCHAR(500) NOT NULL DEFAULT '',
    `summary`        VARCHAR(500) NOT NULL DEFAULT '',
    `description`    MEDIUMTEXT   NULL,
    `price`          BIGINT       NOT NULL DEFAULT 0,
    `original_price` BIGINT       NOT NULL DEFAULT 0,
    `stock`          BIGINT       NOT NULL DEFAULT 0     COMMENT '仅 manual 使用；-1 = 无限',
    `delivery_type`  VARCHAR(16)  NOT NULL DEFAULT 'auto',
    `status`         VARCHAR(16)  NOT NULL DEFAULT 'off',
    `sort`           INT          NOT NULL DEFAULT 0,
    `sales_count`    BIGINT       NOT NULL DEFAULT 0,
    `is_recommend`   TINYINT(1)   NOT NULL DEFAULT 0,
    `min_quantity`   INT          NOT NULL DEFAULT 1,
    `max_quantity`   INT          NOT NULL DEFAULT 100,
    `deleted_at`     DATETIME     NULL,
    `created_at`     DATETIME     NOT NULL,
    `updated_at`     DATETIME     NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_products_slug` (`slug`),
    KEY `idx_products_category` (`category_id`, `status`, `deleted_at`),
    KEY `idx_products_listing` (`deleted_at`, `status`, `sort`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE `product_codes` (
    `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `product_id`    BIGINT UNSIGNED NOT NULL,
    `content`       VARCHAR(1000) NOT NULL,
    `content_hash`  CHAR(64)      NOT NULL COMMENT 'sha256(product_id|content)，同商品去重',
    `status`        VARCHAR(16)   NOT NULL DEFAULT 'unused' COMMENT 'unused | locked | sold',
    `order_id`      BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `order_item_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `locked_at`     DATETIME      NULL,
    `sold_at`       DATETIME      NULL,
    `created_at`    DATETIME      NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_codes_product_hash` (`product_id`, `content_hash`),
    KEY `idx_codes_claim` (`product_id`, `status`, `id`),
    KEY `idx_codes_order` (`order_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE `orders` (
    `id`                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `order_no`           VARCHAR(40)  NOT NULL,
    `query_token`        CHAR(32)     NOT NULL,
    `email`              VARCHAR(190) NOT NULL,
    `original_amount`    BIGINT       NOT NULL DEFAULT 0,
    `discount_amount`    BIGINT       NOT NULL DEFAULT 0,
    `pay_amount`         BIGINT       NOT NULL DEFAULT 0,
    `coupon_id`          BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `coupon_code`        VARCHAR(64)  NOT NULL DEFAULT '',
    `payment_channel_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `payment_method`     VARCHAR(64)  NOT NULL DEFAULT '',
    `payment_provider`   VARCHAR(32)  NOT NULL DEFAULT '',
    `payment_trade_no`   VARCHAR(128) NOT NULL DEFAULT '',
    `status`             VARCHAR(24)  NOT NULL DEFAULT 'pending',
    `delivery_type`      VARCHAR(16)  NOT NULL DEFAULT 'auto',
    `delivery_content`   TEXT         NULL,
    `stock_reserved`     TINYINT(1)   NOT NULL DEFAULT 0,
    `needs_attention`    TINYINT(1)   NOT NULL DEFAULT 0,
    `attention_reason`   VARCHAR(500) NOT NULL DEFAULT '',
    `remark`             TEXT         NULL,
    `client_ip`          VARCHAR(64)  NOT NULL DEFAULT '',
    `refund_amount`      BIGINT       NOT NULL DEFAULT 0,
    `refund_reason`      VARCHAR(500) NOT NULL DEFAULT '',
    `refunded_at`        DATETIME     NULL,
    `paid_at`            DATETIME     NULL,
    `delivered_at`       DATETIME     NULL,
    `expired_at`         DATETIME     NULL,
    `created_at`         DATETIME     NOT NULL,
    `updated_at`         DATETIME     NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_orders_no` (`order_no`),
    UNIQUE KEY `uk_orders_token` (`query_token`),
    KEY `idx_orders_email` (`email`, `created_at`),
    KEY `idx_orders_status` (`status`, `created_at`),
    KEY `idx_orders_expire` (`status`, `expired_at`),
    KEY `idx_orders_trade` (`payment_trade_no`),
    KEY `idx_orders_created` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE `order_items` (
    `id`               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `order_id`         BIGINT UNSIGNED NOT NULL,
    `product_id`       BIGINT UNSIGNED NOT NULL,
    `product_name`     VARCHAR(191) NOT NULL,
    `product_slug`     VARCHAR(150) NOT NULL DEFAULT '',
    `product_cover`    VARCHAR(500) NOT NULL DEFAULT '',
    `product_price`    BIGINT       NOT NULL DEFAULT 0,
    `delivery_type`    VARCHAR(16)  NOT NULL DEFAULT 'auto',
    `quantity`         INT          NOT NULL DEFAULT 1,
    `subtotal`         BIGINT       NOT NULL DEFAULT 0,
    `delivery_content` TEXT         NULL,
    `created_at`       DATETIME     NOT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_order_items_order` (`order_id`),
    KEY `idx_order_items_product` (`product_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE `coupons` (
    `id`             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `code`           VARCHAR(64)  NOT NULL,
    `name`           VARCHAR(191) NOT NULL DEFAULT '',
    `type`           VARCHAR(16)  NOT NULL DEFAULT 'fixed' COMMENT 'fixed | percent',
    `value`          BIGINT       NOT NULL DEFAULT 0 COMMENT 'fixed:分  percent:万分比(9折=9000)',
    `min_amount`     BIGINT       NOT NULL DEFAULT 0,
    `max_discount`   BIGINT       NOT NULL DEFAULT 0,
    `scope`          VARCHAR(16)  NOT NULL DEFAULT 'all' COMMENT 'all | products',
    `usage_limit`    BIGINT       NOT NULL DEFAULT 0 COMMENT '0=不限',
    `used_count`     BIGINT       NOT NULL DEFAULT 0,
    `per_user_limit` BIGINT       NOT NULL DEFAULT 0 COMMENT '0=不限',
    `start_at`       DATETIME     NULL,
    `expire_at`      DATETIME     NULL,
    `status`         VARCHAR(16)  NOT NULL DEFAULT 'active',
    `created_at`     DATETIME     NOT NULL,
    `updated_at`     DATETIME     NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_coupons_code` (`code`),
    KEY `idx_coupons_status` (`status`, `expire_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE `coupon_products` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `coupon_id`  BIGINT UNSIGNED NOT NULL,
    `product_id` BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_coupon_products` (`coupon_id`, `product_id`),
    KEY `idx_coupon_products_product` (`product_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE `coupon_usages` (
    `id`              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `coupon_id`       BIGINT UNSIGNED NOT NULL,
    `order_id`        BIGINT UNSIGNED NOT NULL,
    `order_no`        VARCHAR(40)  NOT NULL DEFAULT '',
    `email`           VARCHAR(190) NOT NULL DEFAULT '',
    `discount_amount` BIGINT       NOT NULL DEFAULT 0,
    `created_at`      DATETIME     NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_coupon_usages` (`coupon_id`, `order_id`),
    KEY `idx_coupon_usages_email` (`coupon_id`, `email`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE `payment_channels` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `name`       VARCHAR(64)  NOT NULL,
    `provider`   VARCHAR(32)  NOT NULL,
    `icon`       VARCHAR(255) NOT NULL DEFAULT '',
    `config`     TEXT         NULL,
    `status`     VARCHAR(16)  NOT NULL DEFAULT 'disabled',
    `sort`       INT          NOT NULL DEFAULT 0,
    `remark`     VARCHAR(500) NOT NULL DEFAULT '',
    `created_at` DATETIME     NOT NULL,
    `updated_at` DATETIME     NOT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_payment_channels_status` (`status`, `sort`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE `payment_logs` (
    `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `order_id`      BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `order_no`      VARCHAR(40)  NOT NULL DEFAULT '',
    `channel_id`    BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `provider`      VARCHAR(32)  NOT NULL DEFAULT '',
    `trade_no`      VARCHAR(128) NOT NULL DEFAULT '',
    `event`         VARCHAR(48)  NOT NULL DEFAULT '',
    `amount`        BIGINT       NOT NULL DEFAULT 0,
    `status`        VARCHAR(32)  NOT NULL DEFAULT '',
    `request_data`  MEDIUMTEXT   NULL,
    `response_data` MEDIUMTEXT   NULL,
    `client_ip`     VARCHAR(64)  NOT NULL DEFAULT '',
    `created_at`    DATETIME     NOT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_payment_logs_order` (`order_id`, `created_at`),
    KEY `idx_payment_logs_event` (`event`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE `system_settings` (
    `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `setting_key` VARCHAR(100) NOT NULL,
    `value`       TEXT         NULL,
    `is_secret`   TINYINT(1)   NOT NULL DEFAULT 0,
    `updated_at`  DATETIME     NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_system_settings_key` (`setting_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE `admin_operation_logs` (
    `id`             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `admin_id`       BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `admin_username` VARCHAR(64)  NOT NULL DEFAULT '',
    `ip`             VARCHAR(64)  NOT NULL DEFAULT '',
    `action`         VARCHAR(64)  NOT NULL DEFAULT '',
    `target_type`    VARCHAR(48)  NOT NULL DEFAULT '',
    `target_id`      VARCHAR(64)  NOT NULL DEFAULT '',
    `detail`         TEXT         NULL,
    `created_at`     DATETIME     NOT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_admin_logs_admin` (`admin_id`, `created_at`),
    KEY `idx_admin_logs_action` (`action`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE `email_logs` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `order_id`   BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `order_no`   VARCHAR(40)  NOT NULL DEFAULT '',
    `to_email`   VARCHAR(190) NOT NULL DEFAULT '',
    `subject`    VARCHAR(255) NOT NULL DEFAULT '',
    `template`   VARCHAR(48)  NOT NULL DEFAULT '',
    `status`     VARCHAR(16)  NOT NULL DEFAULT '',
    `error`      VARCHAR(1000) NOT NULL DEFAULT '',
    `created_at` DATETIME     NOT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_email_logs_order` (`order_id`),
    KEY `idx_email_logs_status` (`status`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
