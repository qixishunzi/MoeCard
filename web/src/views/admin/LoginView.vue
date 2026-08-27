<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError, ErrCode } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useShopStore } from '@/stores/shop'
import { WdButton, WdInput, WdTurnstile } from '@/ui'
import { ArrowLeft, LayoutDashboard } from 'lucide-vue-next'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const shop = useShopStore()

const username = ref('')
const password = ref('')
const errorMsg = ref('')
const submitting = ref(false)

onMounted(async () => {
  // 已登录直接进后台
  if (auth.isLoggedIn && (await auth.fetchProfile())) {
    router.replace((route.query.redirect as string) || { name: 'admin-dashboard' })
  }
})

/**
 * 只有在后端明确要求时才显示验证码输入框。
 * 一上来就显示会让没开 2FA 的用户困惑"我该填什么"。
 */
const needTOTP = ref(false)
const totpCode = ref('')

/** 人机验证。没开启时组件自己不渲染，令牌恒为空，后端也不会校验 */
const captcha = ref('')
const captchaRef = ref<InstanceType<typeof WdTurnstile> | null>(null)

async function submit() {
  if (submitting.value) return
  errorMsg.value = ''

  if (!username.value.trim() || !password.value) {
    errorMsg.value = '请输入用户名和密码'
    return
  }
  if (needTOTP.value && !totpCode.value.trim()) {
    errorMsg.value = '请输入两步验证码'
    return
  }

  if (captchaRef.value?.needed() && !captcha.value) {
    errorMsg.value = '请先完成人机验证'
    return
  }

  submitting.value = true
  try {
    await auth.login(
      username.value.trim(),
      password.value,
      totpCode.value.trim(),
      captcha.value,
    )
    const redirect = (route.query.redirect as string) || ''
    router.replace(
      redirect && redirect.startsWith('/admin') ? redirect : { name: 'admin-dashboard' },
    )
  } catch (e) {
    const err = e as ApiError
    errorMsg.value = err.code === ErrCode.TooManyRequests ? err.message : err.message || '登录失败'
    if (err.code === ErrCode.AdminTOTPRequired || err.code === ErrCode.AdminBadTOTP) {
      // 需要第二因子：展开验证码输入框，密码保留着不清空，
      // 否则用户得把密码重敲一遍才能补验证码
      needTOTP.value = true
      totpCode.value = ''
    } else {
      needTOTP.value = false
      password.value = ''
    }
    // 令牌是一次性的：这次请求无论因为什么失败，那张票都已经作废，
    // 不重置的话下一次提交必然撞上 timeout-or-duplicate
    captchaRef.value?.reset()
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="min-h-screen bg-[#faf8f5] grid place-items-center p-6">
    <div class="w-full max-w-sm">
      <div class="bg-white rounded-2xl shadow-xl shadow-black/[0.04] p-8">
        <div class="flex flex-col items-center">
          <span class="w-11 h-11 rounded-xl bg-[#4a9d9a] grid place-items-center">
            <LayoutDashboard class="w-5 h-5 text-white" />
          </span>
          <h1 class="mt-4 text-lg font-semibold text-gray-800">{{ shop.config.site_name }}</h1>
          <p class="mt-1 text-xs text-gray-400">管理后台</p>
        </div>

        <form class="mt-8 space-y-4" @submit.prevent="submit">
          <WdInput
            v-model="username"
            label="用户名"
            placeholder="管理员用户名"
            @enter="submit"
          />
          <WdInput
            v-model="password"
            type="password"
            label="密码"
            placeholder="登录密码"
            @enter="submit"
          />

          <WdInput
            v-if="needTOTP"
            v-model="totpCode"
            label="两步验证码"
            placeholder="验证器 App 上的 6 位数字，或恢复码"
            mono
            @enter="submit"
          />

          <WdTurnstile ref="captchaRef" v-model="captcha" scene="admin_login" />

          <p v-if="errorMsg" class="text-xs text-[#c17767] leading-relaxed">{{ errorMsg }}</p>

          <WdButton type="submit" variant="primary" block :loading="submitting">
            {{ submitting ? '登录中' : '登 录' }}
          </WdButton>
        </form>

        <p class="mt-5 text-xs text-gray-400 text-center">连续登录失败会被临时限流，请稍后再试。</p>
      </div>

      <RouterLink
        to="/"
        class="mt-6 flex items-center justify-center gap-1.5 text-xs text-gray-400 hover:text-[#4a9d9a] transition-colors duration-200"
      >
        <ArrowLeft class="w-3.5 h-3.5" />
        返回商城
      </RouterLink>
    </div>
  </div>
</template>
