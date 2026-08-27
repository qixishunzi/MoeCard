<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { CheckCircle2, Loader2, XCircle } from 'lucide-vue-next'
import { ApiError, shopApi } from '@/api'
import type { OrderStatusInfo } from '@/api/types'
import { formatAmount } from '@/utils/format'
import { useShopStore } from '@/stores/shop'
import { WdButton, WdCard } from '@/ui'

const route = useRoute()
const shop = useShopStore()

const orderNo = computed(() => String(route.query.order_no || ''))
const status = ref<OrderStatusInfo | null>(null)
const loading = ref(true)
const errorMsg = ref('')
const attempts = ref(0)

let timer: number | undefined

/**
 * 支付结果页的核心原则：**不信任任何 URL 参数**。
 *
 * 支付平台跳回来时可能带 status=success 之类的参数，
 * 但那完全由用户浏览器携带，可以随意伪造。
 * 这里始终以后端查询结果为准 —— 后端会在必要时主动向支付平台核实。
 */
async function check() {
  if (!orderNo.value) {
    errorMsg.value = '缺少订单号'
    loading.value = false
    return
  }
  try {
    status.value = await shopApi.orderStatus(orderNo.value)
    errorMsg.value = ''
  } catch (e) {
    errorMsg.value = e instanceof ApiError ? e.message : '查询失败'
  } finally {
    loading.value = false
  }
}

function startPolling() {
  // 支付平台的异步回调可能有几秒延迟，前 30 秒每 2.5 秒查一次
  timer = window.setInterval(async () => {
    attempts.value++
    await check()
    if (status.value?.paid || attempts.value >= 12) stop()
  }, 2500)
}

function stop() {
  if (timer) {
    clearInterval(timer)
    timer = undefined
  }
}

onMounted(async () => {
  await check()
  if (!status.value?.paid) startPolling()
})
onBeforeUnmount(stop)

const state = computed<'paid' | 'pending' | 'failed'>(() => {
  const s = status.value
  if (!s) return 'failed'
  if (s.paid) return 'paid'
  if (['cancelled', 'expired'].includes(s.status)) return 'failed'
  return 'pending'
})
</script>

<template>
  <div class="max-w-lg mx-auto px-5 sm:px-6 py-8">
    <div v-if="loading" class="py-32 flex justify-center">
      <Loader2 class="w-7 h-7 text-[#4a9d9a] animate-spin" />
    </div>

    <WdCard v-else-if="!status" class="py-12">
      <div class="flex flex-col items-center text-center">
        <span class="w-14 h-14 rounded-2xl bg-gray-100 grid place-items-center">
          <XCircle class="w-7 h-7 text-gray-400" />
        </span>
        <h1 class="mt-4 text-lg font-semibold text-gray-800">无法查询订单</h1>
        <p class="mt-2 text-sm text-gray-500">{{ errorMsg || '订单不存在' }}</p>
      </div>
    </WdCard>

    <WdCard v-else-if="state === 'paid'">
      <div class="flex flex-col items-center text-center">
        <span class="w-16 h-16 rounded-2xl bg-[#4a9d9a]/10 grid place-items-center">
          <CheckCircle2 class="w-8 h-8 text-[#4a9d9a]" />
        </span>
        <h1 class="mt-4 text-xl font-semibold text-gray-800">支付成功</h1>
        <p class="mt-2 text-sm text-gray-500 leading-relaxed">
          <template v-if="status.completed">商品已发货，可以在订单详情中查看卡密。</template>
          <template v-else>我们已收到您的付款，商家会尽快为您发货。</template>
        </p>
      </div>

      <dl class="mt-6 pt-5 border-t border-gray-100 space-y-3 text-sm">
        <div class="flex justify-between gap-6">
          <dt class="text-gray-500 shrink-0">订单号</dt>
          <dd class="font-mono text-xs text-gray-700 break-all text-right">
            {{ status.order_no }}
          </dd>
        </div>
        <div class="flex justify-between gap-6">
          <dt class="text-gray-500">支付金额</dt>
          <dd class="font-semibold text-gray-800 tabular">
            {{ shop.symbol() }}{{ formatAmount(status.pay_amount) }}
          </dd>
        </div>
        <div class="flex justify-between gap-6">
          <dt class="text-gray-500">订单状态</dt>
          <dd class="text-gray-700">{{ status.status_text }}</dd>
        </div>
      </dl>

      <div class="mt-6 flex flex-col sm:flex-row gap-3">
        <WdButton
          variant="primary"
          block
          @click="$router.push({ name: 'order-detail', params: { orderNo: status.order_no } })"
        >
          查看订单详情
        </WdButton>
        <WdButton block @click="$router.push('/')">继续购物</WdButton>
      </div>
    </WdCard>

    <WdCard v-else-if="state === 'pending'">
      <div class="flex flex-col items-center text-center">
        <span class="w-16 h-16 rounded-2xl bg-[#e8b86d]/15 grid place-items-center">
          <Loader2 class="w-8 h-8 text-[#e8b86d] animate-spin" />
        </span>
        <h1 class="mt-4 text-xl font-semibold text-gray-800">正在确认支付结果</h1>
        <p class="mt-2 text-sm text-gray-500 leading-relaxed">
          支付平台的通知可能有几秒延迟，请稍候。<br />
          如果您已完成付款，本页会自动更新。
        </p>
      </div>

      <dl class="mt-6 pt-5 border-t border-gray-100 space-y-3 text-sm">
        <div class="flex justify-between gap-6">
          <dt class="text-gray-500 shrink-0">订单号</dt>
          <dd class="font-mono text-xs text-gray-700 break-all text-right">
            {{ status.order_no }}
          </dd>
        </div>
        <div class="flex justify-between gap-6">
          <dt class="text-gray-500">当前状态</dt>
          <dd class="text-gray-700">{{ status.status_text }}</dd>
        </div>
      </dl>

      <div class="mt-6 flex flex-col sm:flex-row gap-3">
        <WdButton variant="primary" block @click="check">立即刷新</WdButton>
        <WdButton
          block
          @click="$router.push({ name: 'pay', params: { orderNo: status.order_no } })"
        >
          重新支付
        </WdButton>
      </div>

      <p class="mt-4 text-xs text-gray-500 leading-relaxed text-center">
        若长时间未更新，请前往
        <RouterLink :to="{ name: 'order-query' }" class="font-medium text-[#4a9d9a] hover:underline">
          订单查询
        </RouterLink>
        页面用订单号 + 邮箱查看，或联系客服。
      </p>
    </WdCard>

    <WdCard v-else>
      <div class="flex flex-col items-center text-center">
        <span class="w-16 h-16 rounded-2xl bg-[#c17767]/10 grid place-items-center">
          <XCircle class="w-8 h-8 text-[#c17767]" />
        </span>
        <h1 class="mt-4 text-xl font-semibold text-gray-800">
          订单已{{ status.status === 'expired' ? '超时' : '取消' }}
        </h1>
        <p class="mt-2 text-sm text-gray-500">该订单已失效，请重新下单。</p>
        <WdButton class="mt-6" variant="primary" @click="$router.push('/')">返回首页</WdButton>
      </div>
    </WdCard>
  </div>
</template>
