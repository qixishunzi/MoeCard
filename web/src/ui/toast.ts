import { reactive } from 'vue'

/**
 * 轻量消息与确认框。
 *
 * 替代 Element Plus 的 ElMessage / ElMessageBox —— 只保留我们真正用到的能力，
 * 换来的是完全可控的视觉（暖色仪表盘风格）与少掉一个 800KB 的依赖。
 */

export type ToastType = 'success' | 'error' | 'warning' | 'info'

export interface ToastItem {
  id: number
  type: ToastType
  message: string
  /** 剩余毫秒；为 0 表示不自动消失 */
  duration: number
}

export interface ConfirmOptions {
  title?: string
  message: string
  confirmText?: string
  cancelText?: string
  /** danger 会把确认按钮变成陶土红，用于删除等不可逆操作 */
  tone?: 'default' | 'danger'
}

interface ConfirmState extends ConfirmOptions {
  open: boolean
  resolve: ((ok: boolean) => void) | null
}

let seed = 0

export const toastState = reactive<{ items: ToastItem[] }>({ items: [] })

export const confirmState = reactive<ConfirmState>({
  open: false,
  message: '',
  resolve: null,
})

function push(type: ToastType, message: string, duration = 3200) {
  const id = ++seed
  toastState.items.push({ id, type, message, duration })
  if (duration > 0) {
    setTimeout(() => dismissToast(id), duration)
  }
  return id
}

export function dismissToast(id: number) {
  const i = toastState.items.findIndex((t) => t.id === id)
  if (i >= 0) toastState.items.splice(i, 1)
}

export const toast = {
  success: (msg: string, duration?: number) => push('success', msg, duration),
  error: (msg: string, duration?: number) => push('error', msg, duration ?? 4500),
  warning: (msg: string, duration?: number) => push('warning', msg, duration),
  info: (msg: string, duration?: number) => push('info', msg, duration),
}

/**
 * 确认对话框。返回 Promise<boolean>，用户取消时 resolve(false)。
 *
 * 刻意不像 ElMessageBox 那样 reject —— 用 reject 表达"用户点了取消"会让
 * 调用方被迫写 try/catch 包一个根本不是异常的分支。
 */
export function confirmDialog(opts: ConfirmOptions): Promise<boolean> {
  // 上一个还没关就先关掉，避免叠加
  if (confirmState.resolve) confirmState.resolve(false)

  return new Promise<boolean>((resolve) => {
    Object.assign(confirmState, {
      open: true,
      title: opts.title ?? '请确认',
      message: opts.message,
      confirmText: opts.confirmText ?? '确定',
      cancelText: opts.cancelText ?? '取消',
      tone: opts.tone ?? 'default',
      resolve,
    })
  })
}

export function resolveConfirm(ok: boolean) {
  const fn = confirmState.resolve
  confirmState.open = false
  confirmState.resolve = null
  fn?.(ok)
}
