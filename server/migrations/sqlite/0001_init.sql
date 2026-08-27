-- MoeCard 初始化 schema (SQLite)
-- 约定：
--   * 金额一律 INTEGER，单位 = 最小货币单位（人民币 = 分）
--   * 时间一律 DATETIME，存 UTC
--   * 布尔用 INTEGER 0/1

CREATE TABLE admins (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT    NOT NULL,
    password_hash TEXT    NOT NULL,
    nickname      TEXT    NOT NULL DEFAULT '',
    status        TEXT    NOT NULL DEFAULT 'active',
    token_version INTEGER NOT NULL DEFAULT 1,
    last_login_at DATETIME,
    last_login_ip TEXT    NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL
);
CREATE UNIQUE INDEX uk_admins_username ON admins (username);

CREATE TABLE categories (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL,
    slug        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    icon        TEXT    NOT NULL DEFAULT '',
    sort        INTEGER NOT NULL DEFAULT 0,
    status      TEXT    NOT NULL DEFAULT 'active',
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL
);
CREATE UNIQUE INDEX uk_categories_slug ON categories (slug);
CREATE INDEX idx_categories_status_sort ON categories (status, sort);

CREATE TABLE products (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    category_id    INTEGER NOT NULL DEFAULT 0,
    name           TEXT    NOT NULL,
    slug           TEXT    NOT NULL,
    cover          TEXT    NOT NULL DEFAULT '',
    summary        TEXT    NOT NULL DEFAULT '',
    description    TEXT    NOT NULL DEFAULT '',
    price          INTEGER NOT NULL DEFAULT 0,
    original_price INTEGER NOT NULL DEFAULT 0,
    stock          INTEGER NOT NULL DEFAULT 0,       -- 仅 manual 使用；-1 = 无限
    delivery_type  TEXT    NOT NULL DEFAULT 'auto',  -- auto | manual
    status         TEXT    NOT NULL DEFAULT 'off',   -- on | off
    sort           INTEGER NOT NULL DEFAULT 0,
    sales_count    INTEGER NOT NULL DEFAULT 0,
    is_recommend   INTEGER NOT NULL DEFAULT 0,
    min_quantity   INTEGER NOT NULL DEFAULT 1,
    max_quantity   INTEGER NOT NULL DEFAULT 100,
    deleted_at     DATETIME,
    created_at     DATETIME NOT NULL,
    updated_at     DATETIME NOT NULL
);
CREATE UNIQUE INDEX uk_products_slug ON products (slug);
CREATE INDEX idx_products_category ON products (category_id, status, deleted_at);
CREATE INDEX idx_products_listing  ON products (deleted_at, status, sort, id);

-- 卡密：自动发货商品的库存载体
CREATE TABLE product_codes (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    product_id    INTEGER NOT NULL,
    content       TEXT    NOT NULL,
    content_hash  TEXT    NOT NULL,               -- sha256(product_id|content)，用于同商品去重
    status        TEXT    NOT NULL DEFAULT 'unused', -- unused | locked | sold
    order_id      INTEGER NOT NULL DEFAULT 0,
    order_item_id INTEGER NOT NULL DEFAULT 0,
    locked_at     DATETIME,
    sold_at       DATETIME,
    created_at    DATETIME NOT NULL
);
-- 同一商品下卡密内容唯一 —— 从数据库层面杜绝重复导入
CREATE UNIQUE INDEX uk_codes_product_hash ON product_codes (product_id, content_hash);
-- 领卡密热路径：WHERE product_id=? AND status='unused' ORDER BY id
CREATE INDEX idx_codes_claim ON product_codes (product_id, status, id);
CREATE INDEX idx_codes_order ON product_codes (order_id);

CREATE TABLE orders (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    order_no           TEXT    NOT NULL,
    query_token        TEXT    NOT NULL,
    email              TEXT    NOT NULL,
    original_amount    INTEGER NOT NULL DEFAULT 0,
    discount_amount    INTEGER NOT NULL DEFAULT 0,
    pay_amount         INTEGER NOT NULL DEFAULT 0,
    coupon_id          INTEGER NOT NULL DEFAULT 0,
    coupon_code        TEXT    NOT NULL DEFAULT '',
    payment_channel_id INTEGER NOT NULL DEFAULT 0,
    payment_method     TEXT    NOT NULL DEFAULT '',
    payment_provider   TEXT    NOT NULL DEFAULT '',
    payment_trade_no   TEXT    NOT NULL DEFAULT '',
    status             TEXT    NOT NULL DEFAULT 'pending',
    delivery_type      TEXT    NOT NULL DEFAULT 'auto',
    delivery_content   TEXT    NOT NULL DEFAULT '',
    stock_reserved     INTEGER NOT NULL DEFAULT 0,
    needs_attention    INTEGER NOT NULL DEFAULT 0,
    attention_reason   TEXT    NOT NULL DEFAULT '',
    remark             TEXT    NOT NULL DEFAULT '',
    client_ip          TEXT    NOT NULL DEFAULT '',
    refund_amount      INTEGER NOT NULL DEFAULT 0,
    refund_reason      TEXT    NOT NULL DEFAULT '',
    refunded_at        DATETIME,
    paid_at            DATETIME,
    delivered_at       DATETIME,
    expired_at         DATETIME,
    created_at         DATETIME NOT NULL,
    updated_at         DATETIME NOT NULL
);
CREATE UNIQUE INDEX uk_orders_no    ON orders (order_no);
CREATE UNIQUE INDEX uk_orders_token ON orders (query_token);
CREATE INDEX idx_orders_email   ON orders (email, created_at);
CREATE INDEX idx_orders_status  ON orders (status, created_at);
CREATE INDEX idx_orders_expire  ON orders (status, expired_at);
CREATE INDEX idx_orders_trade   ON orders (payment_trade_no);
CREATE INDEX idx_orders_created ON orders (created_at);

CREATE TABLE order_items (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id         INTEGER NOT NULL,
    product_id       INTEGER NOT NULL,
    -- 商品快照：商品被改名/改价/软删都不影响历史订单
    product_name     TEXT    NOT NULL,
    product_slug     TEXT    NOT NULL DEFAULT '',
    product_cover    TEXT    NOT NULL DEFAULT '',
    product_price    INTEGER NOT NULL DEFAULT 0,
    delivery_type    TEXT    NOT NULL DEFAULT 'auto',
    quantity         INTEGER NOT NULL DEFAULT 1,
    subtotal         INTEGER NOT NULL DEFAULT 0,
    delivery_content TEXT    NOT NULL DEFAULT '',
    created_at       DATETIME NOT NULL
);
CREATE INDEX idx_order_items_order   ON order_items (order_id);
CREATE INDEX idx_order_items_product ON order_items (product_id);

CREATE TABLE coupons (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    code           TEXT    NOT NULL,
    name           TEXT    NOT NULL DEFAULT '',
    type           TEXT    NOT NULL DEFAULT 'fixed',  -- fixed | percent
    value          INTEGER NOT NULL DEFAULT 0,        -- fixed: 分；percent: 万分比(9折=9000)
    min_amount     INTEGER NOT NULL DEFAULT 0,
    max_discount   INTEGER NOT NULL DEFAULT 0,
    scope          TEXT    NOT NULL DEFAULT 'all',    -- all | products
    usage_limit    INTEGER NOT NULL DEFAULT 0,        -- 0 = 不限
    used_count     INTEGER NOT NULL DEFAULT 0,
    per_user_limit INTEGER NOT NULL DEFAULT 0,        -- 0 = 不限
    start_at       DATETIME,
    expire_at      DATETIME,
    status         TEXT    NOT NULL DEFAULT 'active',
    created_at     DATETIME NOT NULL,
    updated_at     DATETIME NOT NULL
);
CREATE UNIQUE INDEX uk_coupons_code ON coupons (code);
CREATE INDEX idx_coupons_status ON coupons (status, expire_at);

CREATE TABLE coupon_products (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    coupon_id  INTEGER NOT NULL,
    product_id INTEGER NOT NULL
);
CREATE UNIQUE INDEX uk_coupon_products ON coupon_products (coupon_id, product_id);
CREATE INDEX idx_coupon_products_product ON coupon_products (product_id);

CREATE TABLE coupon_usages (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    coupon_id       INTEGER NOT NULL,
    order_id        INTEGER NOT NULL,
    order_no        TEXT    NOT NULL DEFAULT '',
    email           TEXT    NOT NULL DEFAULT '',
    discount_amount INTEGER NOT NULL DEFAULT 0,
    created_at      DATETIME NOT NULL
);
-- 幂等核心：同一订单对同一优惠券只能核销一次，重复支付回调撞唯一约束
CREATE UNIQUE INDEX uk_coupon_usages ON coupon_usages (coupon_id, order_id);
CREATE INDEX idx_coupon_usages_email ON coupon_usages (coupon_id, email);

CREATE TABLE payment_channels (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    provider   TEXT    NOT NULL,
    icon       TEXT    NOT NULL DEFAULT '',
    config     TEXT    NOT NULL DEFAULT '{}',
    status     TEXT    NOT NULL DEFAULT 'disabled',
    sort       INTEGER NOT NULL DEFAULT 0,
    remark     TEXT    NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
CREATE INDEX idx_payment_channels_status ON payment_channels (status, sort);

CREATE TABLE payment_logs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id      INTEGER NOT NULL DEFAULT 0,
    order_no      TEXT    NOT NULL DEFAULT '',
    channel_id    INTEGER NOT NULL DEFAULT 0,
    provider      TEXT    NOT NULL DEFAULT '',
    trade_no      TEXT    NOT NULL DEFAULT '',
    event         TEXT    NOT NULL DEFAULT '',
    amount        INTEGER NOT NULL DEFAULT 0,
    status        TEXT    NOT NULL DEFAULT '',
    request_data  TEXT    NOT NULL DEFAULT '',
    response_data TEXT    NOT NULL DEFAULT '',
    client_ip     TEXT    NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL
);
CREATE INDEX idx_payment_logs_order ON payment_logs (order_id, created_at);
CREATE INDEX idx_payment_logs_event ON payment_logs (event, created_at);

CREATE TABLE system_settings (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    setting_key TEXT   NOT NULL,
    value      TEXT    NOT NULL DEFAULT '',
    is_secret  INTEGER NOT NULL DEFAULT 0,
    updated_at DATETIME NOT NULL
);
CREATE UNIQUE INDEX uk_system_settings_key ON system_settings (setting_key);

CREATE TABLE admin_operation_logs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    admin_id       INTEGER NOT NULL DEFAULT 0,
    admin_username TEXT    NOT NULL DEFAULT '',
    ip             TEXT    NOT NULL DEFAULT '',
    action         TEXT    NOT NULL DEFAULT '',
    target_type    TEXT    NOT NULL DEFAULT '',
    target_id      TEXT    NOT NULL DEFAULT '',
    detail         TEXT    NOT NULL DEFAULT '',
    created_at     DATETIME NOT NULL
);
CREATE INDEX idx_admin_logs_admin  ON admin_operation_logs (admin_id, created_at);
CREATE INDEX idx_admin_logs_action ON admin_operation_logs (action, created_at);

CREATE TABLE email_logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id   INTEGER NOT NULL DEFAULT 0,
    order_no   TEXT    NOT NULL DEFAULT '',
    to_email   TEXT    NOT NULL DEFAULT '',
    subject    TEXT    NOT NULL DEFAULT '',
    template   TEXT    NOT NULL DEFAULT '',
    status     TEXT    NOT NULL DEFAULT '',
    error      TEXT    NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL
);
CREATE INDEX idx_email_logs_order  ON email_logs (order_id);
CREATE INDEX idx_email_logs_status ON email_logs (status, created_at);
