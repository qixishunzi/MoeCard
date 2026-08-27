/**
 * Cloudflare Turnstile 的全局对象类型声明。
 *
 * 官方没有发 npm 类型包，脚本是运行时从 CDN 注入的，
 * 所以按官方文档把用到的那几个方法自己声明出来。
 * https://developers.cloudflare.com/turnstile/get-started/client-side-rendering/
 */
export interface TurnstileRenderOptions {
  sitekey: string
  theme?: 'auto' | 'light' | 'dark'
  size?: 'normal' | 'flexible' | 'compact'
  action?: string
  appearance?: 'always' | 'execute' | 'interaction-only'
  callback?: (token: string) => void
  'error-callback'?: (code?: string) => void
  'expired-callback'?: () => void
  'timeout-callback'?: () => void
}

export interface TurnstileAPI {
  render(container: string | HTMLElement, options: TurnstileRenderOptions): string
  reset(widgetId?: string): void
  remove(widgetId?: string): void
  getResponse(widgetId?: string): string | undefined
  isExpired(widgetId?: string): boolean
}

declare global {
  interface Window {
    turnstile?: TurnstileAPI
  }
}

export {}
