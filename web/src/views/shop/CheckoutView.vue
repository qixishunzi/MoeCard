<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Loader2, Minus, Package, Plus } from 'lucide-vue-next'
import { ApiError, ErrCode, shopApi } from '@/api'
import type { DiscountResult, PaymentChannel, Product } from '@/api/types'
import { formatAmount } from '@/utils/format'
import { useShopStore } from '@/stores/shop'
import { WdBadge, WdButton, WdCard, WdInput, WdTurnstile } from '@/ui'

const route = useRoute()
const router = useRouter()
const shop = useShopStore()

const product = ref<Product | null>(null)
const channels = ref<PaymentChannel[]>([])
const loading = ref(true)
const submitting = ref(false)
const pageError = ref('')

const quantity = ref(1)
const email = ref('')
const emailConfirm = ref('')
const couponCode = ref('')
const channelId = ref(0)
// 默认不勾选：预先替买家勾上的同意框不构成同意，
// 真起争议时（"我没同意过不退换"）这个勾等于没有。
const agreed = ref(false)

/** 人机验证。未开启该场景时组件不渲染，令牌恒为空 */
const captcha = ref('')
const captchaRef = ref<InstanceType<typeof WdTurnstile> | null>(null)

/** 买家针对商品自定义字段填写的内容 */
const customData = reactive<Record<string, string>>({})
const customFields = computed(() => product.value?.custom_fields ?? [])

const couponState = ref<'idle' | 'checking' | 'ok' | 'error'>('idle')
const couponMsg = ref('')
const discount = ref<DiscountResult | null>(null)
const formError = ref('')

const EMAIL_KEY = 'moecard_last_email'

const originalAmount = computed(() => (product.value?.price ?? 0) * quantity.value)
const discountAmount = computed(() =>
  couponState.value === 'ok' ? (discount.value?.discount_amount ?? 0) : 0,
)
const payAmount = computed(() => Math.max(0, originalAmount.value - discountAmount.value))
const emailValid = computed(() => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.value.trim()))

async function load() {
  loading.value = true
  try {
    const slug = String(route.params.slug)
    const [p, chs] = await Promise.all([
      shopApi.product(slug),
      shopApi.paymentChannels().catch(() => [] as PaymentChannel[]),
    ])
    product.value = p
    channels.value = chs
    for (const f of p.custom_fields ?? []) {
      customData[f.key] = ''
    }
    if (chs.length) channelId.value = chs[0].id

    const q = Number(route.query.quantity) || p.min_quantity || 1
    quantity.value = clamp(q, p)

    // 记住上次填的邮箱，减少重复输入
    try {
      const last = localStorage.getItem(EMAIL_KEY)
      if (last) {
        email.value = last
        emailConfirm.value = last
      }
    } catch {
      /* 隐私模式下不可用，忽略 */
    }
  } catch (e) {
    pageError.value = e instanceof ApiError ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

function clamp(v: number, p: Product) {
  const min = Math.max(1, p.min_quantity || 1)
  const maxLimit = p.max_quantity > 0 ? p.max_quantity : 100
  const max = p.available_stock < 0 ? maxLimit : Math.min(maxLimit, p.available_stock)
  return Math.max(min, Math.min(max, Math.floor(v) || min))
}

function onQtyChange() {
  if (!product.value) return
  quantity.value = clamp(quantity.value, product.value)
  // 数量变了，之前算的优惠不再有效，必须重新校验
  if (couponState.value === 'ok') {
    couponState.value = 'idle'
    discount.value = null
    couponMsg.value = '数量已变更，请重新验证优惠券'
  }
}

async function applyCoupon() {
  const code = couponCode.value.trim()
  if (!code || !product.value) return

  couponState.value = 'checking'
  couponMsg.value = ''
  try {
    const res = await shopApi.verifyCoupon({
      code,
      product_id: product.value.id,
      quantity: quantity.value,
      email: email.value.trim() || undefined,
      turnstile_token: captcha.value || undefined,
    })
    discount.value = res
    couponState.value = 'ok'
    couponMsg.value = `已抵扣 ${shop.symbol()}${formatAmount(res.discount_amount)}`
  } catch (e) {
    discount.value = null
    couponState.value = 'error'
    couponMsg.value = e instanceof ApiError ? e.message : '优惠券验证失败'
  }
}

function clearCoupon() {
  couponCode.value = ''
  discount.value = null
  couponState.value = 'idle'
  couponMsg.value = ''
}

function validate(): boolean {
  formError.value = ''
  if (!emailValid.value) {
    formError.value = '请输入正确的邮箱地址 —— 卡密与订单信息将发送到这里'
    return false
  }
  if (email.value.trim().toLowerCase() !== emailConfirm.value.trim().toLowerCase()) {
    formError.value = '两次输入的邮箱不一致，请检查'
    return false
  }
  for (const f of customFields.value) {
    const v = (customData[f.key] ?? '').trim()
    if (f.required && !v) {
      formError.value = `请填写「${f.label}」`
      return false
    }
    if (v && f.pattern) {
      try {
        if (!new RegExp(f.pattern).test(v)) {
          formError.value = `「${f.label}」格式不正确`
          return false
        }
      } catch {
        // 正则由后端校验过；这里编译失败就交给后端兜底，不阻断下单
      }
    }
  }
  if (!channelId.value) {
    formError.value = '请选择支付方式'
    return false
  }
  if (!agreed.value) {
    formError.value = '请先阅读并同意购买须知'
    return false
  }
  if (captchaRef.value?.needed() && !captcha.value) {
    formError.value = '请先完成人机验证'
    return false
  }
  return true
}

async function submit() {
  if (!product.value || submitting.value) return
  if (!validate()) return

  submitting.value = true
  try {
    const res = await shopApi.createOrder({
      product_id: product.value.id,
      quantity: quantity.value,
      email: email.value.trim(),
      coupon_code: couponState.value === 'ok' ? couponCode.value.trim() : undefined,
      custom_data: customFields.value.length ? { ...customData } : undefined,
      turnstile_token: captcha.value || undefined,
    })

    try {
      localStorage.setItem(EMAIL_KEY, email.value.trim())
      // 查询 token 只在创建订单时返回一次，存下来让用户免邮箱查单
      localStorage.setItem(`moecard_token_${res.order.order_no}`, res.query_token)
    } catch {
      /* 忽略存储失败 */
    }

    router.push({
      name: 'pay',
      params: { orderNo: res.order.order_no },
      query: { channel_id: String(channelId.value) },
    })
  } catch (e) {
    const err = e as ApiError
    formError.value = err.message
    // 令牌一次性，这次提交失败它就作废了，必须换一张
    captchaRef.value?.reset()
    // 库存不足时刷新商品，让用户看到最新库存
    if (err.code === ErrCode.ProductOutOfStock) load()
  } finally {
    submitting.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="max-w-5xl mx-auto px-5 sm:px-6 lg:px-8 py-8">
    <div v-if="loading" class="py-32 flex justify-center">
      <Loader2 class="w-7 h-7 text-[#4a9d9a] animate-spin" />
    </div>

    <WdCard v-else-if="pageError" class="py-16">
      <div class="flex flex-col items-center text-center">
        <p class="text-sm text-gray-500">{{ pageError }}</p>
        <WdButton class="mt-5" variant="primary" size="sm" @click="router.push('/')">
          返回首页
        </WdButton>
      </div>
    </WdCard>

    <template v-else-if="product">
      <h1 class="text-xl font-semibold text-gray-800 mb-5">确认订单</h1>

      <div class="grid lg:grid-cols-[minmax(0,1fr)_340px] gap-5 items-start">
        <div class="space-y-5">
          <!-- 商品 -->
          <WdCard title="商品信息">
            <div class="flex gap-4">
              <img
                v-if="product.cover"
                :src="product.cover"
                :alt="product.name"
                class="w-20 h-20 rounded-xl object-cover shrink-0"
              />
              <div
                v-else
                class="w-20 h-20 rounded-xl shrink-0 grid place-items-center bg-[#faf8f5]"
              >
                <Package class="w-8 h-8 text-gray-300" />
              </div>

              <div class="flex-1 min-w-0">
                <p class="text-sm font-medium text-gray-800 leading-relaxed">{{ product.name }}</p>
                <WdBadge
                  class="mt-2"
                  :tone="product.delivery_type === 'auto' ? 'teal' : 'slate'"
                >
                  {{ product.delivery_type === 'auto' ? '自动发货' : '人工发货' }}
                </WdBadge>
              </div>

              <div class="text-right shrink-0">
                <p class="text-sm font-semibold text-gray-800 tabular">
                  {{ shop.symbol() }}{{ formatAmount(product.price) }}
                </p>
                <div
                  class="mt-2.5 inline-flex items-center rounded-xl border border-gray-200 overflow-hidden"
                >
                  <button
                    type="button"
                    class="w-8 h-8 grid place-items-center text-gray-500 hover:text-[#4a9d9a] hover:bg-[#faf8f5] transition-all duration-200 disabled:opacity-30"
                    :disabled="quantity <= (product.min_quantity || 1)"
                    aria-label="减少数量"
                    @click="
                      quantity--;
                      onQtyChange()
                    "
                  >
                    <Minus class="w-3 h-3" />
                  </button>
                  <input
                    v-model.number="quantity"
                    type="number"
                    inputmode="numeric"
                    aria-label="购买数量"
                    class="w-10 h-8 bg-transparent text-center text-sm font-medium text-gray-800 tabular focus:outline-none [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none"
                    @blur="onQtyChange"
                  />
                  <button
                    type="button"
                    class="w-8 h-8 grid place-items-center text-gray-500 hover:text-[#4a9d9a] hover:bg-[#faf8f5] transition-all duration-200"
                    aria-label="增加数量"
                    @click="
                      quantity++;
                      onQtyChange()
                    "
                  >
                    <Plus class="w-3 h-3" />
                  </button>
                </div>
              </div>
            </div>
          </WdCard>

          <!-- 邮箱 -->
          <WdCard title="接收信息" subtitle="订单信息与卡密都会发送到这里，也是之后查询订单的凭证">
            <div class="grid sm:grid-cols-2 gap-4">
              <WdInput v-model="email" type="email" label="邮箱地址" required />
              <!--
                这个框不用 WdInput：它要禁用粘贴（防止把打错的邮箱一路复制下去），
                而这是它独有的需求，塞进通用输入组件只会污染组件接口。
                但 label/for、aria-describedby 这些必须照样补齐。
              -->
              <div>
                <label
                  for="checkout-email-confirm"
                  class="block mb-1.5 text-xs font-medium text-gray-500"
                >
                  确认邮箱 <span class="text-[#c17767]">*</span>
                </label>
                <input
                  id="checkout-email-confirm"
                  v-model="emailConfirm"
                  type="email"
                  autocomplete="email"
                  aria-describedby="checkout-email-confirm-hint"
                  class="w-full px-3.5 py-2.5 bg-white border border-gray-200 rounded-xl text-sm text-gray-800 placeholder:text-gray-300 transition-all duration-200 focus:outline-none focus:ring-2 focus:ring-[#4a9d9a]/30 focus:border-[#4a9d9a]"
                  @paste.prevent
                />
                <p
                  id="checkout-email-confirm-hint"
                  class="mt-1.5 text-xs text-gray-500 leading-relaxed"
                >
                  为避免打错导致收不到卡密，这里不支持粘贴，请手动输入
                </p>
              </div>
            </div>
          </WdCard>

          <!-- 商品要求的额外信息（代充类必填游戏账号等） -->
          <WdCard
            v-if="customFields.length"
            title="商品所需信息"
            subtitle="商家需要这些信息才能为你发货，请务必填写准确"
          >
            <div class="grid sm:grid-cols-2 gap-4">
              <div
                v-for="f in customFields"
                :key="f.key"
                :class="f.type === 'textarea' && 'sm:col-span-2'"
              >
                <WdInput
                  v-model="customData[f.key]"
                  :type="f.type === 'select' ? 'select' : f.type === 'textarea' ? 'textarea' : 'text'"
                  :label="f.label"
                  :required="f.required"
                  :placeholder="f.placeholder"
                  :maxlength="f.max_len || 200"
                  :rows="3"
                  :options="(f.options ?? []).map((o) => ({ label: o, value: o }))"
                />
              </div>
            </div>
          </WdCard>

          <!-- 优惠券 -->
          <WdCard title="优惠券">
            <div class="flex items-start gap-3 max-w-md">
              <div class="flex-1">
                <WdInput
                  v-model="couponCode"
                  placeholder="有优惠码可在此输入"
                  :disabled="couponState === 'ok'"
                  @enter="applyCoupon"
                />
              </div>
              <WdButton
                v-if="couponState !== 'ok'"
                :loading="couponState === 'checking'"
                :disabled="!couponCode.trim()"
                @click="applyCoupon"
              >
                使用
              </WdButton>
              <WdButton v-else variant="ghost" @click="clearCoupon">取消</WdButton>
            </div>
            <p
              v-if="couponMsg"
              class="mt-3 text-xs"
              :class="couponState === 'ok' ? 'text-[#4a9d9a]' : 'text-[#c17767]'"
            >
              {{ couponMsg }}
            </p>
          </WdCard>

          <!-- 支付方式 -->
          <WdCard title="支付方式">
            <p v-if="!channels.length" class="text-sm text-[#c17767]">
              商家尚未配置任何支付方式，暂时无法下单。
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
                  <span
                    v-if="channelId === ch.id"
                    class="w-2 h-2 rounded-full bg-[#4a9d9a]"
                  />
                </span>
                <img v-if="ch.icon" :src="ch.icon" :alt="ch.name" class="w-5 h-5 object-contain" />
                <span class="text-sm font-medium text-gray-700">{{ ch.name }}</span>
              </label>
            </div>
          </WdCard>
        </div>

        <!-- 汇总 -->
        <WdCard class="lg:sticky lg:top-24" title="费用明细">
          <dl class="space-y-2.5 text-sm">
            <div class="flex justify-between text-gray-500">
              <dt>商品金额</dt>
              <dd class="tabular">{{ shop.symbol() }}{{ formatAmount(originalAmount) }}</dd>
            </div>
            <div class="flex justify-between text-gray-500">
              <dt>数量</dt>
              <dd class="tabular">× {{ quantity }}</dd>
            </div>
            <div v-if="discountAmount > 0" class="flex justify-between text-[#4a9d9a] font-medium">
              <dt>优惠券抵扣</dt>
              <dd class="tabular">− {{ shop.symbol() }}{{ formatAmount(discountAmount) }}</dd>
            </div>
          </dl>

          <div
            class="mt-4 pt-4 border-t border-gray-100 flex items-baseline justify-between"
          >
            <span class="text-sm text-gray-500">应付金额</span>
            <span class="text-2xl font-semibold text-[#4a9d9a] tabular">
              <span class="text-sm font-medium mr-0.5">{{ shop.symbol() }}</span
              >{{ formatAmount(payAmount) }}
            </span>
          </div>

          <label class="mt-5 flex gap-2.5 cursor-pointer">
            <input
              v-model="agreed"
              type="checkbox"
              class="mt-0.5 w-4 h-4 shrink-0 rounded accent-[#4a9d9a]"
            />
            <span class="text-xs text-gray-500 leading-relaxed">
              我已阅读并同意：虚拟商品发货后不支持退换，请确认邮箱无误后下单
            </span>
          </label>

          <WdTurnstile ref="captchaRef" v-model="captcha" scene="order" class="mt-4" />

          <p
            v-if="formError"
            class="mt-4 px-3.5 py-2.5 rounded-xl bg-[#c17767]/10 text-xs text-[#c17767] leading-relaxed"
          >
            {{ formError }}
          </p>

          <WdButton
            class="mt-4"
            variant="primary"
            size="lg"
            block
            :loading="submitting"
            :disabled="!channels.length"
            @click="submit"
          >
            {{ submitting ? '正在创建订单' : '提交订单' }}
          </WdButton>

          <p class="mt-3 text-xs text-center text-gray-500">
            订单将在 {{ shop.config.order_expire_minutes }} 分钟后失效
          </p>
        </WdCard>
      </div>
    </template>
  </div>
</template>
