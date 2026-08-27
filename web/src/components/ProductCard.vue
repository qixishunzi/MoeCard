<script setup lang="ts">
import { computed } from 'vue'
import { Package } from 'lucide-vue-next'
import type { Product } from '@/api/types'
import { formatAmount } from '@/utils/format'
import { useShopStore } from '@/stores/shop'
import { WdBadge } from '@/ui'

const props = defineProps<{ product: Product }>()
const shop = useShopStore()

/** 销量：店主关掉开关时接口就已经不下发了，这里只是不占位 */
const showSales = computed(() => shop.config.show_sales && props.product.sales_count > 0)

const soldOut = computed(() => {
  const p = props.product
  if (p.status !== 'on') return true
  // -1 表示无限库存
  return p.available_stock === 0
})

const stockText = computed(() => {
  const s = props.product.available_stock
  if (s < 0) return '现货充足'
  if (s === 0) return '已售罄'
  if (s <= 5) return `仅剩 ${s} 件`
  return `库存 ${s}`
})

/** 无封面时按商品名做一个稳定的配色，避免每次渲染跳色 */
const tint = computed(() => {
  const palette = ['#4a9d9a', '#e8b86d', '#c17767', '#6b8e8e']
  let sum = 0
  for (const ch of props.product.name) sum += ch.codePointAt(0) ?? 0
  return palette[sum % palette.length]
})
</script>

<template>
  <RouterLink
    :to="{ name: 'product', params: { slug: product.slug } }"
    class="group flex flex-col bg-white rounded-2xl shadow-xl shadow-black/[0.04] overflow-hidden transition-all duration-300 hover:shadow-2xl hover:-translate-y-1"
    :class="soldOut && 'opacity-70'"
  >
    <div class="relative aspect-[4/3] overflow-hidden bg-[#faf8f5] shrink-0">
      <img
        v-if="product.cover"
        :src="product.cover"
        :alt="product.name"
        loading="lazy"
        decoding="async"
        class="w-full h-full object-cover transition-transform duration-300 group-hover:scale-105"
      />
      <div
        v-else
        class="w-full h-full grid place-items-center"
        :style="{ backgroundColor: `${tint}14` }"
      >
        <Package class="w-12 h-12" :style="{ color: tint }" />
      </div>

      <div class="absolute top-3 left-3">
        <WdBadge v-if="soldOut" tone="gray">已售罄</WdBadge>
        <WdBadge v-else-if="product.delivery_type === 'auto'" tone="teal">自动发货</WdBadge>
      </div>
    </div>

    <div class="flex-1 flex flex-col p-4 sm:p-5">
      <h3
        class="text-sm font-medium text-gray-800 leading-relaxed line-clamp-2 min-h-[2.6em] transition-colors duration-200 group-hover:text-[#4a9d9a]"
      >
        {{ product.name }}
      </h3>

      <p v-if="product.summary" class="mt-1.5 text-xs text-gray-500 truncate">
        {{ product.summary }}
      </p>

      <div class="mt-auto pt-4 flex items-end justify-between gap-3">
        <div class="min-w-0">
          <div class="flex items-baseline gap-1.5 flex-wrap">
            <span class="text-lg font-semibold text-gray-800 tabular">
              <span class="text-xs font-medium mr-0.5">{{ shop.symbol() }}</span
              >{{ formatAmount(product.price) }}
            </span>
            <span
              v-if="product.original_price > product.price"
              class="text-xs text-gray-500 line-through tabular"
            >
              {{ shop.symbol() }}{{ formatAmount(product.original_price) }}
            </span>
          </div>
          <p v-if="showSales" class="mt-1 text-[11px] text-gray-500">
            已售 {{ product.sales_count }}
          </p>
        </div>

        <span
          class="text-[11px] font-medium whitespace-nowrap shrink-0"
          :class="
            product.available_stock > 0 && product.available_stock <= 5
              ? 'text-[#8f7243]'
              : 'text-gray-500'
          "
        >
          {{ stockText }}
        </span>
      </div>
    </div>
  </RouterLink>
</template>
