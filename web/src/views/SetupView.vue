<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError, setupApi } from '@/api'
import { useShopStore } from '@/stores/shop'
import { WdButton, WdInput } from '@/ui'
import { CheckCircle2, LayoutDashboard } from 'lucide-vue-next'

const router = useRouter()
const shop = useShopStore()

const siteName = ref('MoeCard')
const username = ref('')
const password = ref('')
const passwordConfirm = ref('')
const submitting = ref(false)
const errorMsg = ref('')
const done = ref(false)
const checking = ref(true)

/**
 * 密码强度提示。规则与后端 utils.ValidatePasswordStrength 保持一致：
 * ≥8 位、至少包含字母/数字/符号中的两类、不在弱口令黑名单里。
 * 前端提示只是体验优化，真正的校验永远在后端。
 */
const strength = computed(() => {
  const p = password.value
  if (!p) return { level: 0, text: '', ok: false }
  const kinds = Number(/[a-zA-Z]/.test(p)) + Number(/\d/.test(p)) + Number(/[^a-zA-Z0-9]/.test(p))
  if (p.length < 8) return { level: 1, text: '太短（至少 8 位）', ok: false }
  if (kinds < 2) return { level: 1, text: '需包含字母、数字、符号中的两类', ok: false }
  if (p.length >= 12 && kinds >= 3) return { level: 3, text: '强', ok: true }
  return { level: 2, text: '中等', ok: true }
})

const canSubmit = computed(
  () =>
    username.value.trim().length >= 3 && strength.value.ok && password.value === passwordConfirm.value,
)

const barCls = computed(() => {
  switch (strength.value.level) {
    case 1:
      return 'w-1/3 bg-[#c17767]'
    case 2:
      return 'w-2/3 bg-[#e8b86d]'
    case 3:
      return 'w-full bg-[#4a9d9a]'
    default:
      return 'w-0'
  }
})

onMounted(async () => {
  try {
    const res = await setupApi.status()
    if (!res.need_setup) {
      router.replace({ name: 'home' })
      return
    }
  } catch {
    /* 拿不到状态就让用户继续，后端会再次校验 */
  } finally {
    checking.value = false
  }
})

async function submit() {
  if (!canSubmit.value || submitting.value) return
  errorMsg.value = ''
  submitting.value = true
  try {
    await setupApi.setup({
      username: username.value.trim(),
      password: password.value,
      site_name: siteName.value.trim() || undefined,
    })
    done.value = true
    await shop.load(true).catch(() => undefined)
    setTimeout(() => router.push({ name: 'admin-login' }), 1500)
  } catch (e) {
    errorMsg.value = e instanceof ApiError ? e.message : '初始化失败'
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="min-h-screen bg-[#faf8f5] grid place-items-center p-6">
    <div class="w-full max-w-md bg-white rounded-2xl shadow-xl shadow-black/[0.04] p-8">
      <div v-if="checking" class="py-16 flex justify-center">
        <svg class="w-6 h-6 animate-spin text-[#4a9d9a]" viewBox="0 0 24 24" fill="none">
          <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="2.5" opacity="0.2" />
          <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" />
        </svg>
      </div>

      <template v-else-if="done">
        <div class="py-8 text-center">
          <span class="w-14 h-14 mx-auto rounded-2xl bg-[#4a9d9a]/10 grid place-items-center">
            <CheckCircle2 class="w-7 h-7 text-[#4a9d9a]" />
          </span>
          <h1 class="mt-5 text-lg font-semibold text-gray-800">初始化完成</h1>
          <p class="mt-2 text-sm text-gray-400">正在跳转到后台登录页…</p>
        </div>
      </template>

      <template v-else>
        <div class="flex items-center gap-3">
          <span class="w-10 h-10 rounded-xl bg-[#4a9d9a] grid place-items-center shrink-0">
            <LayoutDashboard class="w-5 h-5 text-white" />
          </span>
          <div>
            <h1 class="text-lg font-semibold text-gray-800">系统初始化</h1>
            <p class="text-xs text-gray-400">创建第一个管理员账号</p>
          </div>
        </div>

        <p class="mt-5 px-4 py-3 rounded-xl bg-[#faf8f5] text-xs text-gray-500 leading-relaxed">
          为了安全，系统不预置任何默认账号密码。请设置一个足够强的管理员口令。
        </p>

        <form class="mt-6 space-y-4" @submit.prevent="submit">
          <WdInput v-model="siteName" label="商城名称" placeholder="MoeCard" :maxlength="30" />

          <WdInput
            v-model="username"
            label="管理员用户名"
            placeholder="3-32 个字符"
            required
            :maxlength="32"
          />

          <div>
            <WdInput
              v-model="password"
              type="password"
              label="管理员密码"
              placeholder="至少 8 位，包含字母 + 数字/符号"
              required
            />
            <div v-if="password" class="mt-2 flex items-center gap-3">
              <span class="flex-1 h-1 rounded-full bg-gray-100 overflow-hidden">
                <span class="block h-full rounded-full transition-all duration-300" :class="barCls" />
              </span>
              <span
                class="text-xs shrink-0"
                :class="strength.ok ? 'text-gray-400' : 'text-[#c17767]'"
              >
                {{ strength.text }}
              </span>
            </div>
          </div>

          <WdInput
            v-model="passwordConfirm"
            type="password"
            label="确认密码"
            required
            :error="
              passwordConfirm && password !== passwordConfirm ? '两次输入的密码不一致' : undefined
            "
          />

          <p v-if="errorMsg" class="text-xs text-[#c17767] leading-relaxed">{{ errorMsg }}</p>

          <WdButton
            type="submit"
            variant="primary"
            block
            :loading="submitting"
            :disabled="!canSubmit"
          >
            {{ submitting ? '正在初始化' : '完成初始化' }}
          </WdButton>
        </form>

        <p class="mt-6 text-xs text-gray-400 leading-relaxed text-center">
          初始化完成后，请立即在「商城设置」配置站点信息，并在「支付方式」添加支付渠道。
        </p>
      </template>
    </div>
  </div>
</template>
