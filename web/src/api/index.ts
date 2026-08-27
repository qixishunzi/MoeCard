import { api, buildURL, type PageData } from './client'
import type {
  Admin,
  NotifyLog,
  NotifyProviderDesc,
  TOTPSetup,
  TOTPStatus,
  AdminPaymentChannel,
  Category,
  CodeImportResult,
  Coupon,
  CouponUsage,
  CreateOrderResult,
  DashboardStats,
  DiscountResult,
  EmailLog,
  LoginResult,
  OperationLog,
  Order,
  OrderStatusInfo,
  PayResult,
  PaymentChannel,
  PaymentLog,
  PaymentProviderDesc,
  Product,
  ProductCode,
  ProductStock,
  ShopConfig,
  TrendPoint,
  SettingsRuntime,
  BuildInfo,
  UpdateCheck,
} from './types'

/** 前台接口。 */
export const shopApi = {
  config: () => api.get<ShopConfig>('/config'),
  categories: () => api.get<Category[]>('/categories'),
  products: (params: {
    category_id?: number
    keyword?: string
    sort?: string
    recommend?: boolean
    page?: number
    page_size?: number
  }) => api.get<PageData<Product>>('/products', params),
  product: (slug: string) => api.get<Product>(`/products/${encodeURIComponent(slug)}`),
  paymentChannels: () => api.get<PaymentChannel[]>('/payments'),

  verifyCoupon: (data: {
    code: string
    product_id: number
    quantity: number
    email?: string
    turnstile_token?: string
  }) => api.post<DiscountResult>('/coupons/verify', data),

  createOrder: (data: {
    product_id: number
    quantity: number
    email: string
    coupon_code?: string
    custom_data?: Record<string, string>
    turnstile_token?: string
  }) => api.post<CreateOrderResult>('/orders', data),

  queryOrder: (params: {
    order_no?: string
    email?: string
    token?: string
    turnstile_token?: string
  }) => api.get<Order>('/orders/query', params),

  orderStatus: (orderNo: string) =>
    api.get<OrderStatusInfo>(`/orders/${encodeURIComponent(orderNo)}/status`),

  pay: (orderNo: string, data: { channel_id: number; device?: string }) =>
    api.post<PayResult>(`/orders/${encodeURIComponent(orderNo)}/pay`, data),

  cancelOrder: (orderNo: string, email: string) =>
    api.post(`/orders/${encodeURIComponent(orderNo)}/cancel`, { email }),
}

/** 初始化接口。 */
export const setupApi = {
  status: () => api.get<{ need_setup: boolean }>('/setup/status'),
  setup: (data: { username: string; password: string; site_name?: string }) =>
    api.post<{ username: string }>('/setup', data),
}

/** 后台接口。 */
export const adminApi = {
  login: (username: string, password: string, totpCode?: string, turnstileToken?: string) =>
    api.post<LoginResult>('/admin/login', {
      username,
      password,
      totp_code: totpCode || undefined,
      turnstile_token: turnstileToken || undefined,
    }),
  logout: () => api.post('/admin/logout'),
  settingsRuntime: () => api.get<SettingsRuntime>('/admin/settings/runtime'),
  build: () => api.get<BuildInfo>('/admin/build'),
  checkUpdate: () => api.get<UpdateCheck>('/admin/update/check'),
  profile: () => api.get<Admin>('/admin/profile'),
  updateProfile: (body: { nickname?: string; avatar?: string }) =>
    api.put<Admin>('/admin/profile', body),
  changePassword: (old_password: string, new_password: string) =>
    api.put('/admin/profile/password', { old_password, new_password }),

  dashboard: () => api.get<DashboardStats>('/admin/dashboard'),
  trend: (days: number) => api.get<TrendPoint[]>('/admin/dashboard/trend', { days }),

  // 分类
  categories: () => api.get<Category[]>('/admin/categories'),
  createCategory: (data: Partial<Category>) => api.post<Category>('/admin/categories', data),
  updateCategory: (id: number, data: Partial<Category>) =>
    api.put<Category>(`/admin/categories/${id}`, data),
  deleteCategory: (id: number) => api.delete(`/admin/categories/${id}`),
  moveCategoryProducts: (id: number, target: number) =>
    api.post<{ moved: number }>(`/admin/categories/${id}/move`, { target_category_id: target }),

  // 商品
  products: (params: Record<string, unknown>) =>
    api.get<PageData<Product>>('/admin/products', params),
  product: (id: number) => api.get<Product>(`/admin/products/${id}`),
  createProduct: (data: Partial<Product>) => api.post<Product>('/admin/products', data),
  updateProduct: (id: number, data: Partial<Product>) =>
    api.put<Product>(`/admin/products/${id}`, data),
  deleteProduct: (id: number) => api.delete(`/admin/products/${id}`),
  setProductStatus: (id: number, status: 'on' | 'off') =>
    api.post(`/admin/products/${id}/status`, { status }),
  setProductStock: (id: number, stock: number) =>
    api.post(`/admin/products/${id}/stock`, { stock }),
  upload: (file: File) => api.upload<{ url: string }>('/admin/upload', file),

  // 卡密
  codes: (productId: number, params: Record<string, unknown>) =>
    api.get<PageData<ProductCode>>(`/admin/products/${productId}/codes`, params),
  codeStats: (productId: number) =>
    api.get<Record<string, number>>(`/admin/products/${productId}/codes/stats`),
  importCodes: (productId: number, content: string) =>
    api.post<CodeImportResult>(`/admin/products/${productId}/codes`, { content }),
  deleteCodes: (productId: number, data: { ids?: number[]; all_unused?: boolean }) =>
    api.delete<{ deleted: number }>(`/admin/products/${productId}/codes`, data),
  deleteCode: (id: number) => api.delete(`/admin/codes/${id}`),

  // 卡密总览：不绑定商品
  allCodes: (params: Record<string, unknown>) =>
    api.get<PageData<ProductCode>>('/admin/codes', params),
  allCodeStats: () => api.get<Record<string, number>>('/admin/codes/stats'),
  codeInventory: () => api.get<ProductStock[]>('/admin/codes/inventory'),
  importAnyCodes: (productId: number, content: string) =>
    api.post<CodeImportResult>('/admin/codes', { product_id: productId, content }),
  deleteAnyCodes: (ids: number[]) => api.delete<{ deleted: number }>('/admin/codes', { ids }),

  // 订单
  orders: (params: Record<string, unknown>) => api.get<PageData<Order>>('/admin/orders', params),
  order: (id: number) => api.get<Order>(`/admin/orders/${id}`),
  deliverOrder: (id: number, content: string) =>
    api.post<Order>(`/admin/orders/${id}/deliver`, { content }),
  remarkOrder: (id: number, remark: string) => api.post(`/admin/orders/${id}/remark`, { remark }),
  refundOrder: (id: number, data: { amount?: number; reason?: string; manual?: boolean }) =>
    api.post(`/admin/orders/${id}/refund`, data),
  resendMail: (id: number, template?: string) =>
    api.post(`/admin/orders/${id}/resend-mail`, { template }),
  clearAttention: (id: number) => api.delete(`/admin/orders/${id}/attention`),

  // 优惠券
  coupons: (params: Record<string, unknown>) => api.get<PageData<Coupon>>('/admin/coupons', params),
  coupon: (id: number) => api.get<Coupon>(`/admin/coupons/${id}`),
  createCoupon: (data: Partial<Coupon>) => api.post<Coupon>('/admin/coupons', data),
  updateCoupon: (id: number, data: Partial<Coupon>) =>
    api.put<Coupon>(`/admin/coupons/${id}`, data),
  deleteCoupon: (id: number) => api.delete(`/admin/coupons/${id}`),
  couponUsages: (id: number, params: Record<string, unknown>) =>
    api.get<PageData<CouponUsage>>(`/admin/coupons/${id}/usages`, params),

  // 支付
  paymentProviders: () => api.get<PaymentProviderDesc[]>('/admin/payments/providers'),
  paymentChannels: () => api.get<AdminPaymentChannel[]>('/admin/payments'),
  paymentChannel: (id: number) => api.get<AdminPaymentChannel>(`/admin/payments/${id}`),
  createChannel: (data: Record<string, unknown>) => api.post('/admin/payments', data),
  updateChannel: (id: number, data: Record<string, unknown>) =>
    api.put(`/admin/payments/${id}`, data),
  deleteChannel: (id: number) => api.delete(`/admin/payments/${id}`),
  testChannel: (id: number) => api.post<Record<string, unknown>>(`/admin/payments/${id}/test`),

  // 设置
  settings: () => api.get<Record<string, string>>('/admin/settings'),
  updateSettings: (data: Record<string, string>) =>
    api.put<Record<string, string>>('/admin/settings', data),
  testMail: (data: Record<string, unknown>) => api.post('/admin/settings/mail/test', data),

  // 管理员
  admins: (params: Record<string, unknown>) => api.get<PageData<Admin>>('/admin/admins', params),
  createAdmin: (data: Record<string, unknown>) => api.post<Admin>('/admin/admins', data),
  updateAdmin: (id: number, data: Record<string, unknown>) =>
    api.put<Admin>(`/admin/admins/${id}`, data),
  deleteAdmin: (id: number) => api.delete(`/admin/admins/${id}`),

  // 两步验证
  totpStatus: () => api.get<TOTPStatus>('/admin/profile/totp'),
  totpSetup: () => api.post<TOTPSetup>('/admin/profile/totp/setup'),
  totpEnable: (code: string) =>
    api.post<{ recovery_codes: string[] }>('/admin/profile/totp/enable', { code }),
  totpDisable: (password: string) => api.post('/admin/profile/totp/disable', { password }),

  // 商家通知
  notifyProviders: () => api.get<NotifyProviderDesc[]>('/admin/notify/providers'),
  /**
   * 用已保存的 Secret Key 校验一个令牌，确认密钥填对了。
   *
   * 走 raw 拿完整响应：服务端在识别出「你填的是 Cloudflare 测试密钥、
   * 它会放行任何请求」时会把这句话放在 message 里，那句话必须原样给管理员看，
   * 前端不能自己编一句"验证通过"把它盖掉。
   */
  testTurnstile: (token: string) =>
    api.raw<{ hostname: string; challenge_ts: string; testing_key: boolean }>(
      'POST',
      '/admin/turnstile/test',
      { token },
    ),

  testNotify: (channel: string, config?: Record<string, string>) =>
    api.post('/admin/notify/test', { channel, config }),
  notifyLogs: (params: Record<string, unknown>) =>
    api.get<PageData<NotifyLog>>('/admin/logs/notify', params),

  // 导出（返回可直接下载的 URL，由前端带 token 拉取）
  exportOrdersURL: (params: Record<string, unknown>) => buildURL('/admin/orders/export', params),
  exportCodesURL: (productId: number, params: Record<string, unknown>) =>
    buildURL(`/admin/products/${productId}/codes/export`, params),
  exportAllCodesURL: (params: Record<string, unknown>) => buildURL('/admin/codes/export', params),
  download: (url: string, filename: string) => api.download(url, filename),

  // 日志
  operationLogs: (params: Record<string, unknown>) =>
    api.get<PageData<OperationLog>>('/admin/logs/operations', params),
  paymentLogs: (params: Record<string, unknown>) =>
    api.get<PageData<PaymentLog>>('/admin/logs/payments', params),
  emailLogs: (params: Record<string, unknown>) =>
    api.get<PageData<EmailLog>>('/admin/logs/emails', params),
}

export * from './client'
export * from './types'
