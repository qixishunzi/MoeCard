<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Clock, Loader2 } from 'lucide-vue-next'
import { ApiError, shopApi } from '@/api'
import type { Order, PayResult, PaymentChannel } from '@/api/types'
import { formatAmount, formatCountdown } from '@/utils/format'
import { useShopStore } from '@/stores/shop'
import { WdButton, WdCard } from '@/ui'

const route = useRoute()
const router = useRouter()
const shop = useShopStore()

const orderNo = computed(() => String(route.params.orderNo))

const order = ref<Order | null>(null)
const channels = ref<PaymentChannel[]>([])
const channelId = ref(0)
const loading = ref(true)
const paying = ref(false)
const errorMsg = ref('')

const payResult = ref<PayResult | null>(null)
const qrDataUrl = ref('')
const remain = ref(0)

let countdownTimer: number | undefined
let pollTimer: number | undefined

function queryToken(): string | undefined {
  try {
    return localStorage.getItem(`moecard_token_${orderNo.value}`) ?? undefined
  } catch {
    return undefined
  }
}

async function load() {
  loading.value = true
  try {
    const token = queryToken()
    const [o, chs] = await Promise.all([
      shopApi.queryOrder(token ? { token } : { order_no: orderNo.value }),
      shopApi.paymentChannels().catch(() => [] as PaymentChannel[]),
    ])
    order.value = o
    channels.value = chs

    const preset = Number(route.query.channel_id) || 0
    channelId.value = preset || (chs.length ? chs[0].id : 0)

    if (o.status !== 'pending' && o.status !== 'paying') {
      // 已支付 / 已过期 / 已取消，直接去结果页
      router.replace({ name: 'pay-result', query: { order_no: o.order_no } })
      return
    }
    startCountdown()
  } catch (e) {
    errorMsg.value = e instanceof ApiError ? e.message : '订单加载失败'
  } finally {
    loading.value = false
  }
}

function startCountdown() {
  const exp = order.value?.expired_at
  if (!exp) return
  // 后端返回的是商城时区的 "YYYY-MM-DD HH:mm:ss"，
  // 这里换算成本地毫秒时间戳做倒计时
  const target = new Date(exp.replace(' ', 'T')).getTime()
  if (Number.isNaN(target)) return

  const tick = () => {
    remain.value = Math.max(0, Math.floor((target - Date.now()) / 1000))
    if (remain.value <= 0 && countdownTimer) {
      clearInterval(countdownTimer)
      countdownTimer = undefined
    }
  }
  tick()
  countdownTimer = window.setInterval(tick, 1000)
}

async function pay() {
  if (!channelId.value || paying.value) return
  paying.value = true
  errorMsg.value = ''
  qrDataUrl.value = ''

  try {
    const device = window.innerWidth <= 768 ? 'mobile' : 'pc'
    const res = await shopApi.pay(orderNo.value, { channel_id: channelId.value, device })
    payResult.value = res

    if (res.action === 'redirect' && res.url) {
      startPolling()
      window.location.href = res.url
      return
    }
    if (res.action === 'form' && res.form_html) {
      startPolling()
      // 用新窗口打开支付表单，用户支付完还能回到本页看状态。
      // 被拦截时降级为当前页跳转。
      const w = window.open('', '_blank')
      if (w) {
        w.document.open()
        w.document.write(res.form_html)
        w.document.close()
      } else {
        document.open()
        document.write(res.form_html)
        document.close()
      }
      return
    }
    if (res.action === 'qrcode' && res.qrcode) {
      // qrcode 库体积不小，只在真正需要时才动态加载
      const QR = await import('qrcode')
      qrDataUrl.value = await QR.toDataURL(res.qrcode, {
        width: 240,
        margin: 1,
        color: { dark: '#1f2937', light: '#ffffff' },
      })
      startPolling()
      return
    }
    errorMsg.value = '支付渠道返回了无法识别的结果，请更换支付方式'
  } catch (e) {
    errorMsg.value = e instanceof ApiError ? e.message : '发起支付失败'
  } finally {
    paying.value = false
  }
}

/**
 * 轮询订单状态。
 *
 * **绝不信任支付平台的前端跳转参数** —— 那完全可以被伪造。
 * 这里查询的是后端记录，后端会在必要时主动向支付平台核实。
 */
function startPolling() {
  if (pollTimer) return
  pollTimer = window.setInterval(async () => {
    try {
      const s = await shopApi.orderStatus(orderNo.value)
      if (s.paid) {
        stopTimers()
        router.replace({ name: 'pay-result', query: { order_no: orderNo.value } })
      }
    } catch {
      /* 轮询失败静默重试 */
    }
  }, 3000)
}

function stopTimers() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = undefined
  }
  if (countdownTimer) {
    clearInterval(countdownTimer)
    countdownTimer = undefined
  }
}

async function checkNow() {
  try {
    const s = await shopApi.orderStatus(orderNo.value)
    if (s.paid) {
      stopTimers()
      router.replace({ name: 'pay-result', query: { order_no: orderNo.value } })
    } else {
      errorMsg.value = '尚未查询到支付成功，如果已完成付款请稍等片刻再试'
    }
  } catch (e) {
    errorMsg.value = e instanceof ApiError ? e.message : '查询失败'
  }
}

onMounted(load)
onBeforeUnmount(stopTimers)
</script>

<template>
  <div class="max-w-xl mx-auto px-5 sm:px-6 py-8">
    <div v-if="loading" class="py-32 flex justify-center">
      <Loader2 class="w-7 h-7 text-[#4a9d9a] animate-spin" />
    </div>

    <WdCard v-else-if="!order" class="py-16">
      <div class="flex flex-col items-center text-center">
        <p class="text-sm text-gray-500">{{ errorMsg || '订单不存在' }}</p>
        <WdButton class="mt-5" variant="primary" size="sm" @click="router.push('/')">
          返回首页
        </WdButton>
      </div>
    </WdCard>

    <template v-else>
      <h1 class="text-xl font-semibold text-gray-800 mb-5">订单支付</h1>

      <div class="space-y-5">
        <WdCard title="订单摘要">
          <dl class="space-y-2.5 text-sm">
            <div class="flex justify-between gap-6">
              <dt class="text-gray-500 shrink-0">订单号</dt>
              <dd class="font-mono text-xs text-gray-700 break-all text-right">
                {{ order.order_no }}
              </dd>
            </div>
            <div class="flex justify-between gap-6">
              <dt class="text-gray-500 shrink-0">商品</dt>
              <dd class="text-gray-700 text-right">
                {{ order.items[0]?.product_name }} × {{ order.items[0]?.quantity }}
              </dd>
            </div>
            <div class="flex justify-between gap-6">
              <dt class="text-gray-500">商品金额</dt>
              <dd class="text-gray-700 tabular">
                {{ shop.symbol() }}{{ formatAmount(order.original_amount) }}
              </dd>
            </div>
            <div
              v-if="order.discount_amount > 0"
              class="flex justify-between gap-6 text-[#4a9d9a] font-medium"
            >
              <dt>优惠券 {{ order.coupon_code }}</dt>
              <dd class="tabular">− {{ shop.symbol() }}{{ formatAmount(order.discount_amount) }}</dd>
            </div>
          </dl>

          <div class="mt-4 pt-4 border-t border-gray-100 flex items-baseline justify-between">
            <span class="text-sm text-gray-500">应付金额</span>
            <span class="text-3xl font-semibold text-[#4a9d9a] tabular">
              <span class="text-base font-medium mr-0.5">{{ shop.symbol() }}</span
              >{{ formatAmount(order.pay_amount) }}
            </span>
          </div>

          <div
            v-if="remain > 0"
            class="mt-4 flex items-center justify-center gap-2 px-4 py-3 rounded-xl bg-[#e8b86d]/15 text-[#8f7243]"
          >
            <Clock class="w-4 h-4 shrink-0" />
            <span class="text-sm">
              支付剩余时间
              <span class="ml-1 font-semibold tabular">{{ formatCountdown(remain) }}</span>
            </span>
          </div>
          <p
            v-else
            class="mt-4 px-4 py-3 rounded-xl bg-[#c17767]/10 text-center text-sm text-[#c17767]"
          >
            订单已超时，请重新下单
          </p>
        </WdCard>

        <!-- 二维码 -->
        <WdCard v-if="qrDataUrl">
          <div class="flex flex-col items-center">
            <div class="p-3 bg-white rounded-2xl border border-gray-100">
              <img :src="qrDataUrl" alt="支付二维码" class="w-52 h-52" />
            </div>
            <p class="mt-4 text-sm font-medium text-gray-800">请使用对应 App 扫码支付</p>
            <p class="mt-1 text-xs text-gray-500">支付完成后本页会自动跳转</p>
            <WdButton class="mt-5" variant="primary" @click="checkNow">我已完成支付</WdButton>
          </div>
        </WdCard>

        <!-- 选择支付方式 -->
        <template v-else-if="remain > 0">
          <WdCard title="选择支付方式">
            <p v-if="!channels.length" class="text-sm text-[#c17767]">
              商家尚未配置支付方式，请联系客服。
            </p>
            <div v-else class="grid sm:grid-cols-2 gap-3">
              <label
                v-for="ch in channels"
                :key="ch.id"
                class="flex items-center gap-3 px-4 py-3 rounded-xl border cursor-pointer transition-all duration-200"
                :class="
                  channelId === ch.id
                    ? 'border-[#4a9d9a] bg-[#4a9d9a]/[0.06] shadow-md shadow-[#4a9d9a]/10'
                    : 'border-gray-200 hover:border-[#4a9d9a]/40 hover:shadow-sm'
                "
              >
                <input
                  v-model="channelId"
                  type="radio"
                  name="channel"
                  :value="ch.id"
                  class="sr-only"
                />
                <span
                  class="w-4 h-4 rounded-full border-2 shrink-0 grid place-items-center transition-colors duration-200"
                  :class="channelId === ch.id ? 'border-[#4a9d9a]' : 'border-gray-300'"
                >
                  <span v-if="channelId === ch.id" class="w-2 h-2 rounded-full bg-[#4a9d9a]" />
                </span>
                <img v-if="ch.icon" :src="ch.icon" :alt="ch.name" class="w-5 h-5 object-contain" />
                <span class="text-sm font-medium text-gray-700">{{ ch.name }}</span>
              </label>
            </div>

            <p
              v-if="errorMsg"
              class="mt-4 px-3.5 py-2.5 rounded-xl bg-[#c17767]/10 text-xs text-[#c17767] leading-relaxed"
            >
              {{ errorMsg }}
            </p>

            <WdButton
              class="mt-5"
              variant="primary"
              size="lg"
              block
              :loading="paying"
              :disabled="!channelId"
              @click="pay"
            >
              {{ paying ? '正在跳转' : `立即支付 ${shop.symbol()}${formatAmount(order.pay_amount)}` }}
            </WdButton>

            <p class="mt-3 text-center text-xs text-gray-500">
              如果已在其他页面完成支付，
              <button class="font-medium text-[#4a9d9a] hover:underline" @click="checkNow">
                点这里检查支付结果
              </button>
            </p>
          </WdCard>
        </template>

        <div class="flex justify-center gap-6 text-xs text-gray-500">
          <RouterLink
            :to="{ name: 'order-detail', params: { orderNo: order.order_no } }"
            class="hover:text-[#4a9d9a] transition-colors duration-200"
          >
            查看订单详情
          </RouterLink>
          <RouterLink to="/" class="hover:text-[#4a9d9a] transition-colors duration-200">
            返回首页
          </RouterLink>
        </div>
      </div>
    </template>
  </div>
</template>
