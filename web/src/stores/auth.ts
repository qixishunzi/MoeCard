import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { adminApi, clearToken, getToken, setToken, setUnauthorizedHandler } from '@/api'
import type { Admin } from '@/api/types'

/** 管理员登录态。 */
export const useAuthStore = defineStore('auth', () => {
  const token = ref<string>(getToken())
  const admin = ref<Admin | null>(null)
  const loading = ref(false)

  const isLoggedIn = computed(() => !!token.value)

  async function login(
    username: string,
    password: string,
    totpCode?: string,
    turnstileToken?: string,
  ) {
    loading.value = true
    try {
      const res = await adminApi.login(username, password, totpCode, turnstileToken)
      token.value = res.token
      admin.value = res.admin
      setToken(res.token)
      return res
    } finally {
      loading.value = false
    }
  }

  /** 拉取当前登录管理员信息。失败视为登录失效。 */
  async function fetchProfile(): Promise<boolean> {
    if (!token.value) return false
    try {
      admin.value = await adminApi.profile()
      return true
    } catch {
      reset()
      return false
    }
  }

  async function logout() {
    try {
      await adminApi.logout()
    } catch {
      // 登出接口失败不影响本地清理 —— JWT 是无状态的，
      // 前端丢掉 token 就已经登出了
    }
    reset()
  }

  function reset() {
    token.value = ''
    admin.value = null
    clearToken()
  }

  return { token, admin, loading, isLoggedIn, login, logout, fetchProfile, reset }
})

/**
 * 注册 401 处理器。
 *
 * 放在这里而不是 api 层：api 层不应该知道 router 与 store 的存在，
 * 否则会形成 api → store → api 的循环依赖。
 */
export function registerAuthInterceptor(onLogout: () => void) {
  setUnauthorizedHandler(() => {
    const store = useAuthStore()
    store.reset()
    onLogout()
  })
}
