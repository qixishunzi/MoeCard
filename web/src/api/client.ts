import axios, { type AxiosInstance, type AxiosRequestConfig } from 'axios'

/** 统一响应结构，与后端 api.Response 一一对应。 */
export interface ApiResponse<T = unknown> {
  code: number
  message: string
  data: T
}

/** 统一分页结构。 */
export interface PageData<T> {
  list: T[]
  page: number
  page_size: number
  total: number
}

/**
 * 业务错误。
 *
 * 前端只依赖 code 做分支判断，message 仅用于展示 ——
 * 后端随时可以改文案或做国际化，code 才是稳定契约。
 */
export class ApiError extends Error {
  code: number
  constructor(code: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.code = code
  }
}

/** 常用错误码（与 server/internal/api/errcode.go 保持一致）。 */
export const ErrCode = {
  Success: 0,
  BadRequest: 40001,
  Validation: 40002,
  Unauthorized: 40101,
  TokenExpired: 40102,
  Forbidden: 40301,
  NotFound: 40401,
  TooManyRequests: 42901,
  Maintenance: 45301,
  Internal: 50001,

  AdminTOTPRequired: 49108,
  CaptchaRequired: 49301,
  CaptchaFailed: 49302,
  CaptchaMisconfig: 49303,
  AdminBadTOTP: 49109,

  ProductNotFound: 45001,
  ProductOffShelf: 45002,
  ProductOutOfStock: 45003,

  CouponInvalid: 46001,
  CouponExpired: 46002,
  CouponNotStarted: 46003,
  CouponUsedUp: 46004,
  CouponNotApplicable: 46005,
  CouponMinAmount: 46006,
  CouponUserLimit: 46007,
  CouponDisabled: 46008,

  OrderNotFound: 47001,
  OrderExpired: 47002,
  OrderStatusInvalid: 47003,
  OrderAlreadyPaid: 47004,
  ShopClosed: 47008,

  PaymentChannelNotFound: 48001,
  PaymentFailed: 48002,
  RefundNotSupported: 48008,

  AlreadySetup: 49107,
} as const

const TOKEN_KEY = 'moecard_admin_token'

export function getToken(): string {
  try {
    return localStorage.getItem(TOKEN_KEY) ?? ''
  } catch {
    return ''
  }
}

export function setToken(token: string) {
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token)
    else localStorage.removeItem(TOKEN_KEY)
  } catch {
    /* 隐私模式下 localStorage 可能不可用，忽略即可 */
  }
}

export function clearToken() {
  setToken('')
}

/** 登录失效时的回调，由 auth store 注册，避免 api 层直接依赖 router。 */
let onUnauthorized: (() => void) | null = null
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

const http: AxiosInstance = axios.create({
  baseURL: '/api/v1',
  timeout: 30000,
  headers: { 'Content-Type': 'application/json' },
})

http.interceptors.request.use((config) => {
  // 后台接口带 JWT。用 Authorization 头而非 Cookie，天然免疫 CSRF。
  const token = getToken()
  if (token && config.url?.startsWith('/admin')) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

http.interceptors.response.use(
  (response) => response,
  (error) => {
    // 网络层错误统一转成 ApiError，让调用方只需处理一种错误类型
    if (error.response?.data && typeof error.response.data.code === 'number') {
      return Promise.reject(error)
    }
    if (error.code === 'ECONNABORTED') {
      return Promise.reject(new ApiError(ErrCode.Internal, '请求超时，请检查网络后重试'))
    }
    if (!error.response) {
      return Promise.reject(new ApiError(ErrCode.Internal, '网络连接失败，请检查网络'))
    }
    return Promise.reject(new ApiError(ErrCode.Internal, `服务异常 (HTTP ${error.response.status})`))
  },
)

/** 发起请求并解包 data。失败时抛出 ApiError。 */
export async function request<T = unknown>(config: AxiosRequestConfig): Promise<T> {
  return (await requestFull<T>(config)).data
}

/**
 * 同 request，但连 message 一起返回。
 *
 * 绝大多数接口的成功提示前端自己写就够了；少数接口的 message 带着
 * 前端不可能知道的信息（比如「你填的是测试密钥，它会放行任何请求」），
 * 那种就得原样透出来，不能被一句写死的"操作成功"盖掉。
 */
export async function requestFull<T = unknown>(
  config: AxiosRequestConfig,
): Promise<{ data: T; message: string }> {
  try {
    const res = await http.request<ApiResponse<T>>(config)
    const body = res.data
    if (body.code !== ErrCode.Success) {
      throw new ApiError(body.code, body.message)
    }
    return { data: body.data, message: body.message }
  } catch (err) {
    if (err instanceof ApiError) throw err

    const anyErr = err as { response?: { data?: ApiResponse } }
    const body = anyErr.response?.data
    if (body && typeof body.code === 'number') {
      // 登录失效：清 token 并通知上层跳登录页
      if (body.code === ErrCode.Unauthorized || body.code === ErrCode.TokenExpired) {
        clearToken()
        onUnauthorized?.()
      }
      throw new ApiError(body.code, body.message)
    }
    throw new ApiError(ErrCode.Internal, (err as Error).message || '请求失败')
  }
}

export const api = {
  get: <T = unknown>(url: string, params?: Record<string, unknown>) =>
    request<T>({ method: 'GET', url, params }),
  /** 需要拿到服务端 message 时用它 */
  raw: <T = unknown>(method: 'GET' | 'POST' | 'PUT' | 'DELETE', url: string, data?: unknown) =>
    requestFull<T>({ method, url, data }),
  post: <T = unknown>(url: string, data?: unknown) =>
    request<T>({ method: 'POST', url, data }),
  put: <T = unknown>(url: string, data?: unknown) =>
    request<T>({ method: 'PUT', url, data }),
  delete: <T = unknown>(url: string, data?: unknown) =>
    request<T>({ method: 'DELETE', url, data }),
  upload: <T = unknown>(url: string, file: File) => {
    const form = new FormData()
    form.append('file', file)
    return request<T>({
      method: 'POST',
      url,
      data: form,
      headers: { 'Content-Type': 'multipart/form-data' },
      timeout: 60000,
    })
  },

  /**
   * 下载文件（CSV 导出）。
   *
   * 不能直接用 <a href> —— 导出接口需要 Authorization 头，
   * 而浏览器不会给普通链接带上它。这里用 blob 中转，
   * 顺带能把后端返回的业务错误（JSON）识别出来提示用户。
   */
  download: async (url: string, filename: string): Promise<void> => {
    const resp = await http.get(url, { responseType: 'blob', timeout: 120000 })
    const blob = resp.data as Blob

    // 出错时后端返回的是 JSON 而不是 CSV
    if (blob.type.includes('application/json')) {
      const text = await blob.text()
      try {
        const j = JSON.parse(text)
        throw new ApiError(j.code ?? ErrCode.Internal, j.message || '导出失败')
      } catch (e) {
        if (e instanceof ApiError) throw e
        throw new ApiError(ErrCode.Internal, '导出失败')
      }
    }

    const href = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = href
    a.download = filename
    document.body.appendChild(a)
    a.click()
    a.remove()
    // 立刻撤销会让部分浏览器来不及开始下载
    setTimeout(() => URL.revokeObjectURL(href), 1000)
  },
}

/** buildURL 把查询参数拼到路径上（供导出接口使用）。 */
export function buildURL(path: string, params?: Record<string, unknown>): string {
  if (!params) return path
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') q.append(k, String(v))
  }
  const qs = q.toString()
  return qs ? `${path}?${qs}` : path
}

export default http
