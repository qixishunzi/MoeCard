<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ChevronRight, Loader2, Minus, Package, Plus } from 'lucide-vue-next'
import { ApiError, shopApi } from '@/api'
import type { Product } from '@/api/types'
import { formatAmount } from '@/utils/format'
import { useShopStore } from '@/stores/shop'
import { WdBadge, WdButton, WdCard } from '@/ui'

const route = useRoute()
const router = useRouter()
const shop = useShopStore()

const product = ref<Product | null>(null)
const loading = ref(true)
const error = ref('')
const quantity = ref(1)

const minQty = computed(() => Math.max(1, product.value?.min_quantity ?? 1))

const maxQty = computed(() => {
  const p = product.value
  if (!p) return 1
  const byLimit = p.max_quantity > 0 ? p.max_quantity : 100
  // 无限库存（-1）时只受单笔上限约束
  if (p.available_stock < 0) return byLimit
  return Math.max(1, Math.min(byLimit, p.available_stock))
})

const soldOut = computed(() => {
  const p = product.value
  return !p || p.status !== 'on' || p.available_stock === 0
})

const canBuy = computed(() => !soldOut.value && shop.config.allow_order)
const subtotal = computed(() => (product.value?.price ?? 0) * quantity.value)

const stockText = computed(() => {
  const p = product.value
  if (!p) return ''
  if (p.available_stock < 0) return '现货充足'
  return `${p.available_stock} 件`
})

const tint = computed(() => {
  const palette = ['#4a9d9a', '#e8b86d', '#c17767', '#6b8e8e']
  let sum = 0
  for (const ch of product.value?.name ?? '') sum += ch.codePointAt(0) ?? 0
  return palette[sum % palette.length]
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    const slug = String(route.params.slug)
    product.value = await shopApi.product(slug)
    quantity.value = minQty.value
    shop.applyDocumentMeta(product.value.name)
  } catch (e) {
    product.value = null
    error.value = e instanceof ApiError ? e.message : '商品加载失败'
  } finally {
    loading.value = false
  }
}

function clampQty() {
  let v = Math.floor(Number(quantity.value) || minQty.value)
  if (v < minQty.value) v = minQty.value
  if (v > maxQty.value) v = maxQty.value
  quantity.value = v
}

function buy() {
  if (!product.value || !canBuy.value) return
  clampQty()
  router.push({
    name: 'checkout',
    params: { slug: product.value.slug },
    query: { quantity: String(quantity.value) },
  })
}

watch(() => route.params.slug, load)
onMounted(load)
</script>

<template>
  <div class="max-w-6xl mx-auto px-5 sm:px-6 lg:px-8 py-8">
    <div v-if="loading" class="py-32 flex justify-center">
      <Loader2 class="w-7 h-7 text-[#4a9d9a] animate-spin" />
    </div>

    <WdCard v-else-if="error" class="py-16">
      <div class="flex flex-col items-center text-center">
        <p class="text-sm text-gray-500">{{ error }}</p>
        <WdButton class="mt-5" variant="primary" size="sm" @click="router.push('/')">
          返回首页
        </WdButton>
      </div>
    </WdCard>

    <template v-else-if="product">
      <!-- 面包屑 -->
      <nav class="flex items-center gap-1.5 text-xs text-gray-500 mb-5">
        <RouterLink to="/" class="hover:text-[#4a9d9a] transition-colors duration-200">
          首页
        </RouterLink>
        <ChevronRight class="w-3 h-3 shrink-0" />
        <template v-if="product.category_name">
          <RouterLink
            :to="{ name: 'home', query: { category_id: String(product.category_id) } }"
            class="hover:text-[#4a9d9a] transition-colors duration-200"
          >
            {{ product.category_name }}
          </RouterLink>
          <ChevronRight class="w-3 h-3 shrink-0" />
        </template>
        <span class="text-gray-500 truncate">{{ product.name }}</span>
      </nav>

      <div class="grid lg:grid-cols-[minmax(0,1fr)_380px] gap-5 items-start">
        <!-- 图 + 详情 -->
        <div class="space-y-5">
          <WdCard flush>
            <div class="rounded-2xl overflow-hidden">
              <img
                v-if="product.cover"
                :src="product.cover"
                :alt="product.name"
                class="w-full aspect-[16/10] object-cover"
              />
              <div
                v-else
                class="w-full aspect-[16/10] grid place-items-center"
                :style="{ backgroundColor: `${tint}14` }"
              >
                <Package class="w-20 h-20" :style="{ color: tint }" />
              </div>
            </div>
          </WdCard>

          <WdCard v-if="product.description" title="商品详情">
            <!-- 描述由后端做过 XSS 白名单净化，这里只负责渲染排版 -->
            <div class="wd-rich text-sm" v-html="product.description" />
          </WdCard>
        </div>

        <!-- 购买栏 -->
        <WdCard class="lg:sticky lg:top-24">
          <div class="flex items-start justify-between gap-3">
            <h1 class="text-xl font-semibold text-gray-800 leading-snug">{{ product.name }}</h1>
            <WdBadge :tone="product.delivery_type === 'auto' ? 'teal' : 'slate'">
              {{ product.delivery_type === 'auto' ? '自动发货' : '人工发货' }}
            </WdBadge>
          </div>

          <p v-if="product.summary" class="mt-2.5 text-sm text-gray-500 leading-relaxed">
            {{ product.summary }}
          </p>

          <div class="mt-5 px-4 py-4 rounded-xl bg-[#faf8f5]">
            <div class="flex items-baseline gap-2.5 flex-wrap">
              <span class="text-3xl font-semibold text-gray-800 tabular">
                <span class="text-base font-medium mr-0.5">{{ shop.symbol() }}</span
                >{{ formatAmount(product.price) }}
              </span>
              <span
                v-if="product.original_price > product.price"
                class="text-sm text-gray-500 line-through tabular"
              >
                {{ shop.symbol() }}{{ formatAmount(product.original_price) }}
              </span>
            </div>
          </div>

          <dl class="mt-5 space-y-3 text-sm">
            <div class="flex gap-4">
              <dt class="w-16 shrink-0 text-gray-500">库存</dt>
              <dd :class="soldOut ? 'text-[#c17767] font-medium' : 'text-gray-700'">
                {{ soldOut ? '已售罄' : stockText }}
              </dd>
            </div>
            <div v-if="shop.config.show_sales && product.sales_count > 0" class="flex gap-4">
              <dt class="w-16 shrink-0 text-gray-500">累计销量</dt>
              <dd class="text-gray-700 tabular">{{ product.sales_count }}</dd>
            </div>
          </dl>

          <template v-if="!soldOut">
            <div class="mt-5 pt-5 border-t border-gray-100 flex items-center gap-4">
              <span class="text-sm text-gray-500 shrink-0">数量</span>
              <div class="inline-flex items-center rounded-xl border border-gray-200 overflow-hidden">
                <button
                  type="button"
                  class="w-9 h-9 grid place-items-center text-gray-500 hover:text-[#4a9d9a] hover:bg-[#faf8f5] transition-all duration-200 disabled:opacity-30 disabled:cursor-not-allowed"
                  :disabled="quantity <= minQty"
                  aria-label="减少数量"
                  @click="quantity = Math.max(minQty, quantity - 1)"
                >
                  <Minus class="w-3.5 h-3.5" />
                </button>
                <input
                  v-model.number="quantity"
                  type="number"
                  inputmode="numeric"
                  :min="minQty"
                  :max="maxQty"
                  aria-label="购买数量"
                  class="w-12 h-9 bg-transparent text-center text-sm font-medium text-gray-800 tabular focus:outline-none [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
                  @blur="clampQty"
                />
                <button
                  type="button"
                  class="w-9 h-9 grid place-items-center text-gray-500 hover:text-[#4a9d9a] hover:bg-[#faf8f5] transition-all duration-200 disabled:opacity-30 disabled:cursor-not-allowed"
                  :disabled="quantity >= maxQty"
                  aria-label="增加数量"
                  @click="quantity = Math.min(maxQty, quantity + 1)"
                >
                  <Plus class="w-3.5 h-3.5" />
                </button>
              </div>
              <span v-if="minQty > 1 || maxQty < 100" class="text-xs text-gray-500">
                {{ minQty }}~{{ maxQty }} 件
              </span>
            </div>

            <div class="mt-5 flex items-baseline justify-between">
              <span class="text-sm text-gray-500">合计</span>
              <span class="text-2xl font-semibold text-[#4a9d9a] tabular">
                <span class="text-sm font-medium mr-0.5">{{ shop.symbol() }}</span
                >{{ formatAmount(subtotal) }}
              </span>
            </div>

            <WdButton
              class="mt-5"
              variant="primary"
              size="lg"
              block
              :disabled="!canBuy"
              @click="buy"
            >
              {{ shop.config.allow_order ? '立即购买' : '暂停下单' }}
            </WdButton>

            <p
              v-if="!shop.config.allow_order"
              class="mt-3 text-xs text-center text-[#8f7243]"
            >
              商城当前暂停下单
            </p>
          </template>

          <p
            v-else
            class="mt-5 px-4 py-3.5 rounded-xl bg-[#faf8f5] text-sm text-gray-500 leading-relaxed"
          >
            该商品暂时缺货，请稍后再来，或看看其他商品。
          </p>
        </WdCard>
      </div>
    </template>
  </div>
</template>
