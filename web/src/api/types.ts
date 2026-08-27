/**
 * 与后端 DTO 对应的类型定义。
 *
 * 约定：所有金额字段单位都是**分**（int64），展示时用 utils/money.ts 转换。
 */

/** 后端下发的人机验证配置。site_key 是公开信息，密钥永远不会出现在这里 */
export interface TurnstileConfig {
  enabled: boolean
  site_key: string
  size: 'normal' | 'flexible' | 'compact'
  on_admin_login: boolean
  on_order: boolean
  on_order_query: boolean
  on_coupon: boolean
}

/** 一条客服联系方式。url / action 由服务端算好，前端不必知道各家的链接规则 */
export interface ShopContact {
  type: 'telegram' | 'whatsapp' | 'wechat' | 'qq' | 'email'
  value: string
  label?: string
  /** 有值就是可点击跳转的链接 */
  url?: string
  /** link = 跳转，copy = 复制 */
  action?: 'link' | 'copy'
}

/** 首页轮播图。product_slug 有值才可点击跳转 */
export interface ShopBanner {
  image: string
  title?: string
  product_slug?: string
  product_name?: string
}

export interface ShopConfig {
  site_name: string
  site_title: string
  site_description: string
  site_keywords: string
  site_logo: string
  site_notice: string
  site_footer: string
  contacts: ShopContact[]
  banners: ShopBanner[]
  /** 公告弹窗 */
  notice_popup: boolean
  notice_force_read: boolean
  notice_force_seconds: number
  icp: string
  currency: string
  currency_symbol: string
  timezone: string
  allow_order: boolean
  show_sales: boolean
  maintenance: boolean
  maintenance_text: string
  order_expire_minutes: number
  installed: boolean
  turnstile?: TurnstileConfig
}

export interface Category {
  id: number
  name: string
  slug: string
  description: string
  icon: string
  sort: number
  status: 'active' | 'disabled'
  product_count?: number
  created_at?: string
  updated_at?: string
}

export type DeliveryType = 'auto' | 'manual'
export type ProductStatus = 'on' | 'off'

export interface Product {
  id: number
  category_id: number
  category_name?: string
  name: string
  slug: string
  cover: string
  summary: string
  description: string
  price: number
  original_price: number
  stock: number
  available_stock: number
  delivery_type: DeliveryType
  status: ProductStatus
  sort: number
  sales_count: number
  is_recommend: boolean
  min_quantity: number
  max_quantity: number
  /** 低库存告警阈值，0 表示沿用全局设置 */
  low_stock_threshold: number
  /** 买家下单时需额外填写的字段定义 */
  custom_fields: CustomField[] | null
  created_at?: string
  updated_at?: string
}

export type OrderStatus =
  | 'pending'
  | 'paying'
  | 'paid'
  | 'waiting_delivery'
  | 'completed'
  | 'cancelled'
  | 'expired'
  | 'refunded'

export interface OrderItem {
  id?: number
  product_id?: number
  product_name: string
  product_slug?: string
  product_cover?: string
  product_price: number
  quantity: number
  subtotal: number
  delivery_type: DeliveryType
  delivery_content?: string
}

export interface OrderCustomField {
  key: string
  label: string
  value: string
}

export interface Order {
  id?: number
  order_no: string
  email: string
  status: OrderStatus
  status_text: string
  original_amount: number
  discount_amount: number
  pay_amount: number
  coupon_code: string
  payment_method: string
  payment_provider?: string
  trade_no: string
  delivery_type: DeliveryType
  delivery_content?: string
  needs_attention?: boolean
  attention_reason?: string
  remark?: string
  /** 买家下单时填写的自定义信息（字段 key -> 值） */
  /** 买家下单时填写的信息。后端已配好标签，只在订单详情里返回 */
  custom_data?: OrderCustomField[] | null
  client_ip?: string
  refund_amount: number
  refund_reason?: string
  refunded_at?: string
  created_at: string
  paid_at: string
  delivered_at: string
  expired_at: string
  items: OrderItem[]
}

export interface OrderStatusInfo {
  order_no: string
  status: OrderStatus
  status_text: string
  paid: boolean
  completed: boolean
  pay_amount: number
}

export interface DiscountResult {
  coupon_id: number
  coupon_code: string
  coupon_name: string
  original_amount: number
  discount_amount: number
  pay_amount: number
}

export interface PaymentChannel {
  id: number
  name: string
  provider: string
  icon: string
  sort: number
}

export interface PayResult {
  action: 'redirect' | 'qrcode' | 'form'
  url?: string
  qrcode?: string
  form_html?: string
  order_no: string
}

export interface CreateOrderResult {
  order: Order
  query_token: string
}

// ---- 后台 ----

export interface Admin {
  id: number
  username: string
  nickname: string
  /** 头像地址，空表示用用户名首字母 */
  avatar: string
  status: 'active' | 'disabled'
  last_login_at: string | null
  last_login_ip: string
  created_at: string
}

/** 这份二进制的身份，反馈问题时要带上 */
export interface BuildInfo {
  version: string
  build_time: string
  commit: string
}

/** 检查更新的结果 */
export interface UpdateCheck {
  current: string
  latest?: string
  has_update?: boolean
  /** 这个平台/运行方式能不能自更新（容器里就不能） */
  supported?: boolean
  reason?: string
  release?: {
    version: string
    tag: string
    name: string
    notes: string
    url: string
    published_at: string
    asset_name: string
  }
  /** 连不上 GitHub 时给出原因，不当成错误 */
  error?: string
}

/** 设置页要用的运行期事实，不是配置项 */
export interface SettingsRuntime {
  trust_proxy: boolean
  builtin_ip_headers: string[]
  detected_ip: string
  detected_from: string
}

export interface LoginResult {
  token: string
  expires_at: string
  admin: Admin
}

export type CouponType = 'fixed' | 'percent'
export type CouponScope = 'all' | 'products'

export interface Coupon {
  id: number
  code: string
  name: string
  type: CouponType
  /** fixed: 分；percent: 万分比（9 折 = 9000） */
  value: number
  min_amount: number
  max_discount: number
  scope: CouponScope
  usage_limit: number
  used_count: number
  per_user_limit: number
  start_at: string | null
  expire_at: string | null
  status: 'active' | 'disabled'
  product_ids?: number[]
  products?: Product[]
  created_at?: string
}

export interface CouponUsage {
  id: number
  coupon_id: number
  order_id: number
  order_no: string
  email: string
  discount_amount: number
  created_at: string
}

export interface ProductCode {
  id: number
  product_id: number
  /** 只有跨商品的卡密总览才返回 */
  product_name?: string
  content: string
  masked_content: string
  status: 'unused' | 'locked' | 'sold'
  order_id: number
  order_no?: string
  locked_at: string | null
  sold_at: string | null
  created_at: string
}

export interface ConfigFieldOption {
  label: string
  value: string
}

export interface ConfigField {
  key: string
  label: string
  type: 'text' | 'password' | 'textarea' | 'select' | 'number' | 'switch'
  required: boolean
  secret: boolean
  placeholder?: string
  help?: string
  options?: ConfigFieldOption[]
  default?: string
}

/** 商品自定义字段定义 */
export interface CustomField {
  key: string
  label: string
  type: 'text' | 'textarea' | 'select'
  required: boolean
  placeholder?: string
  options?: string[]
  max_len?: number
  pattern?: string
}

/** 通知渠道的配置字段（结构与支付渠道一致，但没有 select/number） */
export interface NotifyConfigField {
  key: string
  label: string
  type: 'text' | 'password' | 'switch'
  required: boolean
  secret: boolean
  placeholder?: string
  help?: string
}

export interface NotifyProviderDesc {
  key: string
  name: string
  fields: NotifyConfigField[]
  note?: string
}

export interface NotifyLog {
  id: number
  channel: string
  event: string
  title: string
  content: string
  status: 'success' | 'failed'
  error: string
  created_at: string
}

export interface TOTPStatus {
  enabled: boolean
  recovery_remaining: number
}

export interface TOTPSetup {
  secret: string
  uri: string
}

export interface PaymentProviderDesc {
  key: string
  name: string
  fields: ConfigField[]
  can_refund: boolean
  note?: string
}

export interface AdminPaymentChannel {
  id: number
  name: string
  provider: string
  icon: string
  status: 'enabled' | 'disabled'
  sort: number
  remark: string
  config: Record<string, string>
  notify_url: string
  available: boolean
  created_at?: string
  updated_at?: string
}

export interface RecentOrder {
  id: number
  order_no: string
  email: string
  product_name: string
  quantity: number
  pay_amount: number
  status: OrderStatus
  status_text: string
  /** 已按商城时区格式化的 "YYYY-MM-DD HH:mm:ss" */
  created_at: string
}

export interface DashboardStats {
  today_orders: number
  today_revenue: number
  yesterday_revenue: number
  total_revenue: number
  month_revenue: number
  pending_orders: number
  paid_orders: number
  waiting_delivery: number
  completed_orders: number
  total_orders: number
  product_count: number
  on_sale_count: number
  code_stock: number
  needs_attention: number
  recent_orders: RecentOrder[]
}

export interface TrendPoint {
  date: string
  orders: number
  revenue: number
}

export interface OperationLog {
  id: number
  admin_id: number
  admin_username: string
  ip: string
  action: string
  target_type: string
  target_id: string
  detail: string
  created_at: string
}

export interface PaymentLog {
  id: number
  order_id: number
  order_no: string
  channel_id: number
  provider: string
  trade_no: string
  event: string
  amount: number
  status: string
  request_data: string
  response_data: string
  client_ip: string
  created_at: string
}

export interface EmailLog {
  id: number
  order_id: number
  order_no: string
  to_email: string
  subject: string
  template: string
  status: 'success' | 'failed'
  error: string
  created_at: string
}

export interface ProductStock {
  product_id: number
  product_name: string
  unused: number
  locked: number
  sold: number
}

export interface CodeImportResult {
  total: number
  imported: number
  duplicate: number
  samples: string[] | null
}
