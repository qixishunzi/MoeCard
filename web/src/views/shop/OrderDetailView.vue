<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Copy, Loader2, Package } from 'lucide-vue-next'
import { ApiError, ErrCode, shopApi } from '@/api'
import type { Order } from '@/api/types'
import { copyText, formatAmount } from '@/utils/format'
import { useShopStore } from '@/stores/shop'
import { WdBadge, WdButton, WdCard, WdInput, confirmDialog, toast } from '@/ui'

const route = useRoute()
const router = useRouter()
const shop = useShopStore()

const orderNo = computed(() => String(route.params.orderNo))
const order = ref<Order | null>(null)
const loading = ref(true)
const errorMsg = ref('')
const needAuth = ref(false)
const emailInput = ref('')
const cancelling = ref(false)

function storedToken(): string | undefined {
  try {
    return localStorage.getItem(`moecard_token_${orderNo.value}`) ?? undefined
  } catch {
    return undefined
  }
}

async function load() {
  // 订单查询页可能已经查到过了（那次请求带着一次性的人机验证令牌）。
  // 令牌用一次就作废，这里再查一遍必然过不了验证，所以直接接住它带过来的结果。
  const handed = (history.state as { order?: Order } | null)?.order
  if (handed && handed.order_no === orderNo.value) {
    order.value = handed
    needAuth.value = false
    loading.value = false
    // 用掉即清，避免刷新时拿到一份越来越旧的快照
    history.replaceState({ ...history.state, order: undefined }, '')
    return
  }

  loading.value = true
  errorMsg.value = ''
  try {
    // 优先用 URL 里的 token（邮件链接），再用本地缓存的 token，
    // 最后回退到「订单号 + 邮箱」
    const urlToken = String(route.query.token || '')
    const token = urlToken || storedToken()
    const email = String(route.query.email || '') || emailInput.value.trim()

    if (token) {
      order.value = await shopApi.queryOrder({ token })
    } else if (email) {
      order.value = await shopApi.queryOrder({ order_no: orderNo.value, email })
      try {
        localStorage.setItem('moecard_last_email', email)
      } catch {
        /* 忽略 */
      }
    } else {
      needAuth.value = true
      return
    }
    needAuth.value = false
  } catch (e) {
    const err = e as ApiError
    errorMsg.value = err.message
    // 这个页面上没有验证控件（控件在查询页）。被人机验证拦下时
    // 把用户送回查询页去验证，而不是留在这里干瞪眼。
    if (err.code === ErrCode.CaptchaRequired || err.code === ErrCode.CaptchaFailed) {
      router.replace({ name: 'order-query', query: { order_no: orderNo.value } })
      return
    }
    needAuth.value = true
    order.value = null
  } finally {
    loading.value = false
  }
}

async function verifyByEmail() {
  if (!emailInput.value.trim()) {
    errorMsg.value = '请输入下单邮箱'
    return
  }
  await load()
}

async function copy(text: string, label: string) {
  const ok = await copyText(text)
  ok ? toast.success(`${label}已复制`) : toast.error('复制失败')
}

async function cancelOrder() {
  if (!order.value) return
  const ok = await confirmDialog({
    title: '取消订单',
    message: '确定要取消这个订单吗？取消后库存会被释放。',
    confirmText: '取消订单',
    cancelText: '再想想',
    tone: 'danger',
  })
  if (!ok) return

  cancelling.value = true
  try {
    await shopApi.cancelOrder(order.value.order_no, order.value.email)
    toast.success('订单已取消')
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '取消失败')
  } finally {
    cancelling.value = false
  }
}

const deliveryContent = computed(() => {
  const o = order.value
  if (!o) return ''
  const fromItems = o.items
    .map((i) => i.delivery_content)
    .filter((c): c is string => !!c && c.trim() !== '')
  if (fromItems.length) return fromItems.join('\n')
  return o.delivery_content ?? ''
})

/** 买家下单时填写的自定义信息。标签由后端按商品字段定义配好 */
const customEntries = computed(() => order.value?.custom_data ?? [])

const statusTone = computed<'teal' | 'amber' | 'clay' | 'slate' | 'gray'>(() => {
  switch (order.value?.status) {
    case 'completed':
      return 'teal'
    case 'paid':
    case 'waiting_delivery':
      return 'slate'
    case 'pending':
    case 'paying':
      return 'amber'
    case 'refunded':
      return 'clay'
    default:
      return 'gray'
  }
})

const statusHint = computed(() => {
  const o = order.value
  if (!o) return ''
  switch (o.status) {
    case 'pending':
    case 'paying':
      return `订单尚未支付，请在 ${o.expired_at} 前完成支付`
    case 'waiting_delivery':
      return '已收到您的付款，商家会尽快发货，发货后会发送邮件通知'
    case 'completed':
      return '订单已完成，发货内容见下方'
    case 'expired':
      return '订单超时未支付已失效'
    case 'refunded':
      return `已退款 ${shop.symbol()}${formatAmount(o.refund_amount)}`
    default:
      return '订单已取消'
  }
})

onMounted(load)
</script>

<template>
  <div class="max-w-3xl mx-auto px-5 sm:px-6 py-8">
    <div v-if="loading" class="py-32 flex justify-center">
      <Loader2 class="w-7 h-7 text-[#4a9d9a] animate-spin" />
    </div>

    <!-- 需要邮箱验证 -->
    <div v-else-if="needAuth" class="max-w-sm mx-auto py-12">
      <WdCard title="验证订单归属" subtitle="为保护隐私，查看订单需要验证下单邮箱">
        <WdInput
          v-model="emailInput"
          type="email"
          label="下单邮箱"
          required
          :error="errorMsg"
          @enter="verifyByEmail"
        />
        <WdButton class="mt-5" variant="primary" block @click="verifyByEmail">验证并查看</WdButton>
        <p class="mt-4 text-center text-xs text-gray-500">
          订单号 <span class="font-mono">{{ orderNo }}</span>
        </p>
      </WdCard>
    </div>

    <template v-else-if="order">
      <div class="flex items-center justify-between gap-4 mb-5">
        <h1 class="text-xl font-semibold text-gray-800">订单详情</h1>
        <RouterLink
          :to="{ name: 'order-query' }"
          class="text-xs font-medium text-[#4a9d9a] hover:underline"
        >
          查询其他订单
        </RouterLink>
      </div>

      <div class="space-y-5">
        <!-- 状态 -->
        <WdCard>
          <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
            <div class="min-w-0">
              <WdBadge :tone="statusTone" dot>{{ order.status_text }}</WdBadge>
              <p class="mt-2.5 text-sm text-gray-500 leading-relaxed">{{ statusHint }}</p>
            </div>

            <div
              v-if="order.status === 'pending' || order.status === 'paying'"
              class="flex gap-2.5 shrink-0"
            >
              <WdButton
                variant="primary"
                @click="$router.push({ name: 'pay', params: { orderNo: order.order_no } })"
              >
                去支付
              </WdButton>
              <WdButton :loading="cancelling" @click="cancelOrder">取消订单</WdButton>
            </div>
          </div>
        </WdCard>

        <!-- 发货内容 -->
        <WdCard v-if="deliveryContent">
          <template #header>
            <h2 class="text-lg font-semibold text-gray-800">
              {{ order.delivery_type === 'auto' ? '卡密内容' : '发货内容' }}
            </h2>
          </template>
          <template #actions>
            <WdButton size="sm" @click="copy(deliveryContent, '内容')">
              <Copy class="w-3.5 h-3.5" />
              复制全部
            </WdButton>
          </template>

          <pre
            class="px-4 py-3.5 rounded-xl bg-[#faf8f5] font-mono text-[13px] leading-relaxed text-gray-700 whitespace-pre-wrap break-all max-h-80 overflow-auto"
            >{{ deliveryContent }}</pre
          >
          <p class="mt-3 text-xs text-gray-500 leading-relaxed">
            请妥善保管以上内容。虚拟商品发出后不支持退换，如有问题请及时联系客服。
          </p>
        </WdCard>

        <!-- 商品 -->
        <WdCard title="商品信息">
          <div
            v-for="(it, idx) in order.items"
            :key="idx"
            class="flex items-center gap-4 py-3 border-b border-gray-100 last:border-0"
          >
            <img
              v-if="it.product_cover"
              :src="it.product_cover"
              :alt="it.product_name"
              class="w-14 h-14 rounded-xl object-cover shrink-0"
            />
            <div v-else class="w-14 h-14 rounded-xl shrink-0 grid place-items-center bg-[#faf8f5]">
              <Package class="w-6 h-6 text-gray-300" />
            </div>
            <div class="flex-1 min-w-0">
              <p class="text-sm font-medium text-gray-800">{{ it.product_name }}</p>
              <p class="mt-0.5 text-xs text-gray-500 tabular">
                {{ shop.symbol() }}{{ formatAmount(it.product_price) }} × {{ it.quantity }}
              </p>
            </div>
            <p class="text-sm font-medium text-gray-800 tabular shrink-0">
              {{ shop.symbol() }}{{ formatAmount(it.subtotal) }}
            </p>
          </div>

          <dl class="mt-4 space-y-2.5 text-sm">
            <div class="flex justify-between text-gray-500">
              <dt>商品金额</dt>
              <dd class="tabular">{{ shop.symbol() }}{{ formatAmount(order.original_amount) }}</dd>
            </div>
            <div
              v-if="order.discount_amount > 0"
              class="flex justify-between text-[#4a9d9a] font-medium"
            >
              <dt>优惠券 {{ order.coupon_code }}</dt>
              <dd class="tabular">− {{ shop.symbol() }}{{ formatAmount(order.discount_amount) }}</dd>
            </div>
          </dl>

          <div class="mt-4 pt-4 border-t border-gray-100 flex items-baseline justify-between">
            <span class="text-sm text-gray-500">
              {{ order.status === 'refunded' ? '已退款' : '实付金额' }}
            </span>
            <span class="text-2xl font-semibold text-gray-800 tabular">
              <span class="text-sm font-medium mr-0.5">{{ shop.symbol() }}</span
              >{{ formatAmount(order.pay_amount) }}
            </span>
          </div>
        </WdCard>

        <!-- 订单信息 -->
        <!-- 买家下单时填写的信息，回显出来便于核对 -->
        <WdCard v-if="customEntries.length" title="你填写的信息">
          <dl class="space-y-3.5 text-sm">
            <div v-for="f in customEntries" :key="f.key" class="flex gap-4">
              <dt class="w-24 shrink-0 text-gray-500">{{ f.label }}</dt>
              <dd class="text-gray-700 break-all whitespace-pre-wrap">{{ f.value }}</dd>
            </div>
          </dl>
        </WdCard>

        <WdCard title="订单信息">
          <dl class="space-y-3.5 text-sm">
            <div class="flex gap-4">
              <dt class="w-20 shrink-0 text-gray-500">订单号</dt>
              <dd class="flex items-center gap-2 min-w-0">
                <span class="font-mono text-xs text-gray-700 break-all">{{ order.order_no }}</span>
                <button
                  class="shrink-0 p-1 rounded-lg text-gray-300 hover:text-[#4a9d9a] hover:bg-[#faf8f5] transition-all duration-200"
                  aria-label="复制订单号"
                  @click="copy(order.order_no, '订单号')"
                >
                  <Copy class="w-3.5 h-3.5" />
                </button>
              </dd>
            </div>
            <div class="flex gap-4">
              <dt class="w-20 shrink-0 text-gray-500">邮箱</dt>
              <dd class="text-gray-700 break-all">{{ order.email }}</dd>
            </div>
            <div class="flex gap-4">
              <dt class="w-20 shrink-0 text-gray-500">发货方式</dt>
              <dd class="text-gray-700">
                {{ order.delivery_type === 'auto' ? '自动发货' : '人工发货' }}
              </dd>
            </div>
            <div v-if="order.payment_method" class="flex gap-4">
              <dt class="w-20 shrink-0 text-gray-500">支付方式</dt>
              <dd class="text-gray-700">{{ order.payment_method }}</dd>
            </div>
            <div v-if="order.trade_no" class="flex gap-4">
              <dt class="w-20 shrink-0 text-gray-500">支付流水</dt>
              <dd class="font-mono text-xs text-gray-600 break-all">{{ order.trade_no }}</dd>
            </div>
            <div class="flex gap-4">
              <dt class="w-20 shrink-0 text-gray-500">创建时间</dt>
              <dd class="text-gray-700 tabular">{{ order.created_at }}</dd>
            </div>
            <div v-if="order.paid_at" class="flex gap-4">
              <dt class="w-20 shrink-0 text-gray-500">支付时间</dt>
              <dd class="text-gray-700 tabular">{{ order.paid_at }}</dd>
            </div>
            <div v-if="order.delivered_at" class="flex gap-4">
              <dt class="w-20 shrink-0 text-gray-500">发货时间</dt>
              <dd class="text-gray-700 tabular">{{ order.delivered_at }}</dd>
            </div>
          </dl>
        </WdCard>
      </div>
    </template>

    <WdCard v-else class="py-16">
      <div class="flex flex-col items-center text-center">
        <p class="text-sm text-gray-500">{{ errorMsg || '订单不存在' }}</p>
        <WdButton
          class="mt-5"
          variant="primary"
          size="sm"
          @click="$router.push({ name: 'order-query' })"
        >
          重新查询
        </WdButton>
      </div>
    </WdCard>
  </div>
</template>
