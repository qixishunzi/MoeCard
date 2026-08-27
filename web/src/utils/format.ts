/**
 * 金额处理。
 *
 * 后端所有金额都是**分**（int64）。前端只在展示时转换，
 * 任何计算都必须用整数分进行 —— 一旦转成浮点元再算，就会出现
 * 0.1 + 0.2 !== 0.3 这类经典问题，最终变成对不上账。
 */

/** 把分转成两位小数字符串：1000 → "10.00" */
export function formatAmount(cents: number | undefined | null): string {
  const v = Math.trunc(Number(cents ?? 0))
  const neg = v < 0
  const abs = Math.abs(v)
  const s = `${Math.floor(abs / 100)}.${String(abs % 100).padStart(2, '0')}`
  return neg ? `-${s}` : s
}

/** 带货币符号：1000 → "¥10.00" */
export function formatMoney(cents: number | undefined | null, symbol = '¥'): string {
  return symbol + formatAmount(cents)
}

/** 把用户输入的元字符串转成分。纯字符串解析，不经过浮点。 */
export function parseAmount(input: string | number): number {
  const s = String(input).trim()
  if (!s) return 0
  const neg = s.startsWith('-')
  const body = s.replace(/^[-+]/, '')
  const [intPart = '0', fracPart = ''] = body.split('.')
  const whole = parseInt(intPart || '0', 10)
  if (Number.isNaN(whole)) return 0
  // 多于两位小数直接截断，不四舍五入 —— 宁可少收不可多收
  const frac = parseInt((fracPart + '00').slice(0, 2), 10) || 0
  const total = whole * 100 + frac
  return neg ? -total : total
}

/** 万分比 → 折扣文案：9000 → "9 折"，9500 → "9.5 折" */
export function formatPercentOff(value: number): string {
  const discount = value / 1000
  const s = Number.isInteger(discount) ? String(discount) : discount.toFixed(1)
  return `${s} 折`
}

/** 折扣输入（如 9 或 9.5）→ 万分比 */
export function discountToPercent(input: number): number {
  return Math.round(input * 1000)
}

/**
 * 时间展示。
 *
 * 后端返回的时间已经按商城时区转换成 "YYYY-MM-DD HH:mm:ss" 字符串，
 * 前端不再做时区转换，避免二次转换导致错位。
 * 只有 ISO 字符串（如 created_at）才需要在这里格式化。
 */
export function formatDateTime(input: string | null | undefined): string {
  if (!input) return '—'
  // 后端已格式化好的字符串直接返回
  if (/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}(:\d{2})?$/.test(input)) return input

  const d = new Date(input)
  if (Number.isNaN(d.getTime())) return input
  const pad = (n: number) => String(n).padStart(2, '0')
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
    `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
  )
}

/** 相对时间：用于"还剩 x 分 x 秒" */
export function formatCountdown(seconds: number): string {
  if (seconds <= 0) return '00:00'
  const m = Math.floor(seconds / 60)
  const s = seconds % 60
  if (m >= 60) {
    const h = Math.floor(m / 60)
    return `${h}:${String(m % 60).padStart(2, '0')}:${String(s).padStart(2, '0')}`
  }
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}

/** 订单状态 → 展示用的颜色标签 */
export function orderStatusType(status: string): 'success' | 'warning' | 'danger' | 'info' | 'primary' {
  switch (status) {
    case 'completed':
      return 'success'
    case 'paid':
    case 'waiting_delivery':
      return 'primary'
    case 'pending':
    case 'paying':
      return 'warning'
    case 'cancelled':
    case 'expired':
      return 'info'
    case 'refunded':
      return 'danger'
    default:
      return 'info'
  }
}

export const ORDER_STATUS_LABELS: Record<string, string> = {
  pending: '待付款',
  paying: '支付中',
  paid: '已支付',
  waiting_delivery: '待发货',
  completed: '已完成',
  cancelled: '已取消',
  expired: '已过期',
  refunded: '已退款',
}

/** 库存展示：-1 表示无限 */
export function formatStock(stock: number): string {
  return stock < 0 ? '无限' : String(stock)
}

/** 简单的防抖。 */
export function debounce<T extends (...args: never[]) => void>(fn: T, wait = 300) {
  let timer: ReturnType<typeof setTimeout> | undefined
  return (...args: Parameters<T>) => {
    if (timer) clearTimeout(timer)
    timer = setTimeout(() => fn(...args), wait)
  }
}

/** 复制到剪贴板，带降级方案。 */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    /* 继续走降级方案 */
  }
  try {
    // HTTP 环境下 navigator.clipboard 不可用，用老办法兜底
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch {
    return false
  }
}
