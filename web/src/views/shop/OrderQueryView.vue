<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Search } from 'lucide-vue-next'
import { ApiError, shopApi } from '@/api'
import { WdButton, WdCard, WdInput, WdTurnstile } from '@/ui'

const route = useRoute()
const router = useRouter()

const orderNo = ref('')
const email = ref('')
const loading = ref(false)
const errorMsg = ref('')

/** 人机验证。未开启该场景时组件不渲染，令牌恒为空 */
const captcha = ref('')
const captchaRef = ref<InstanceType<typeof WdTurnstile> | null>(null)

onMounted(() => {
  // 从订单详情页被人机验证挡回来时会带上订单号，预填省得重敲
  const from = String(route.query.order_no || '').trim()
  if (from) orderNo.value = from
  try {
    const last = localStorage.getItem('moecard_last_email')
    if (last) email.value = last
  } catch {
    /* 忽略 */
  }
})

async function query() {
  errorMsg.value = ''
  const no = orderNo.value.trim()
  const mail = email.value.trim()
  if (!no || !mail) {
    errorMsg.value = '请填写订单号与下单邮箱'
    return
  }

  if (captchaRef.value?.needed() && !captcha.value) {
    errorMsg.value = '请先完成人机验证'
    return
  }

  loading.value = true
  try {
    // 这里直接查一次，查得到再跳详情页 —— 让错误提示留在表单上，
    // 用户不必进到详情页才发现订单号打错了。
    //
    // 详情页会用同一个令牌再查一次吗？不会：人机验证的令牌是一次性的，
    // 所以这次用掉之后，跳转时把「已经查到的订单」一并带过去，
    // 详情页直接用，不再发第二次请求。
    const order = await shopApi.queryOrder({
      order_no: no,
      email: mail,
      turnstile_token: captcha.value || undefined,
    })
    try {
      localStorage.setItem('moecard_last_email', mail)
    } catch {
      /* 忽略 */
    }
    router.push({
      name: 'order-detail',
      params: { orderNo: no },
      query: { email: mail },
      state: { order: JSON.parse(JSON.stringify(order)) },
    })
  } catch (e) {
    errorMsg.value = e instanceof ApiError ? e.message : '查询失败'
    // 令牌一次性，失败后必须换一张，否则下次提交必然撞重放校验
    captchaRef.value?.reset()
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="max-w-md mx-auto px-5 sm:px-6 py-8 space-y-5">
    <WdCard title="订单查询" subtitle="输入下单时的订单号与邮箱，即可查看订单状态与卡密">
      <form class="space-y-4" @submit.prevent="query">
        <WdInput v-model="orderNo" label="订单号" required mono placeholder="下单后生成的 24 位编号" />

        <WdInput
          v-model="email"
          type="email"
          label="下单邮箱"
          required
          hint="为保护隐私，必须同时提供订单号与邮箱才能查看订单"
        />

        <WdTurnstile ref="captchaRef" v-model="captcha" scene="order_query" />

        <p
          v-if="errorMsg"
          class="px-3.5 py-2.5 rounded-xl bg-[#c17767]/10 text-xs text-[#c17767] leading-relaxed"
        >
          {{ errorMsg }}
        </p>

        <WdButton type="submit" variant="primary" size="lg" block :loading="loading">
          <Search v-if="!loading" class="w-4 h-4" />
          {{ loading ? '查询中' : '查询订单' }}
        </WdButton>
      </form>
    </WdCard>

    <WdCard title="找不到订单？">
      <ul class="space-y-2 text-xs text-gray-500 leading-relaxed list-disc list-inside">
        <li>检查订单号是否完整（24 位字母数字）</li>
        <li>确认邮箱与下单时填写的完全一致</li>
        <li>订单邮件可能在垃圾邮件箱中，邮件里也有直达链接</li>
      </ul>
    </WdCard>
  </div>
</template>
