<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ChevronLeft, ChevronRight, Inbox, Megaphone, Sparkles } from 'lucide-vue-next'
import HomeBanner from '@/components/HomeBanner.vue'
import { shopApi } from '@/api'
import type { Product } from '@/api/types'
import { useShopStore } from '@/stores/shop'
import ProductCard from '@/components/ProductCard.vue'
import { WdButton, WdTabs } from '@/ui'

const route = useRoute()
const router = useRouter()
const shop = useShopStore()

const products = ref<Product[]>([])
const recommends = ref<Product[]>([])
const loading = ref(true)
const total = ref(0)
const page = ref(1)
const pageSize = 24

const keyword = computed(() => (route.query.keyword as string) || '')
const categoryId = computed(() => Number(route.query.category_id) || 0)
const sort = computed(() => (route.query.sort as string) || 'default')

const activeCategory = computed(() => shop.categories.find((c) => c.id === categoryId.value))

const sortOptions = [
  { value: 'default', label: '综合' },
  { value: 'newest', label: '最新' },
  { value: 'sales', label: '销量' },
  { value: 'price_asc', label: '价格 ↑' },
  { value: 'price_desc', label: '价格 ↓' },
]

/**
 * 请求序号，用来丢弃过期结果。
 *
 * 搜索框现在是边打边搜，短时间内会发好几次请求。慢的那次要是后到，
 * 会把新关键词的结果覆盖成旧的 —— 屏幕上的商品和输入框里的词对不上。
 */
let reqSeq = 0

async function loadProducts() {
  const seq = ++reqSeq
  loading.value = true
  try {
    const res = await shopApi.products({
      keyword: keyword.value || undefined,
      category_id: categoryId.value || undefined,
      sort: sort.value,
      page: page.value,
      page_size: pageSize,
    })
    if (seq !== reqSeq) return
    products.value = res.list ?? []
    total.value = res.total
  } catch {
    if (seq !== reqSeq) return
    products.value = []
    total.value = 0
  } finally {
    if (seq === reqSeq) loading.value = false
  }
}

async function loadRecommends() {
  // 只有在首页（无筛选）时才展示推荐位
  if (keyword.value || categoryId.value) {
    recommends.value = []
    return
  }
  try {
    const res = await shopApi.products({ recommend: true, page_size: 8 })
    recommends.value = res.list ?? []
  } catch {
    recommends.value = []
  }
}

function setQuery(patch: Record<string, string | number | undefined>) {
  const q: Record<string, string> = {}
  for (const [k, v] of Object.entries({ ...route.query, ...patch })) {
    if (v !== undefined && v !== '' && v !== null) q[k] = String(v)
  }
  router.push({ name: 'home', query: q })
}

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))

const listTitle = computed(() => {
  if (keyword.value) return `搜索「${keyword.value}」`
  if (activeCategory.value) return activeCategory.value.name
  return '全部商品'
})

watch(
  () => route.query,
  () => {
    page.value = Number(route.query.page) || 1
    loadProducts()
    loadRecommends()
  },
)

onMounted(() => {
  page.value = Number(route.query.page) || 1
  loadProducts()
  loadRecommends()
})
</script>

<template>
  <div class="max-w-6xl mx-auto px-5 sm:px-6 lg:px-8 py-8 space-y-6">
    <!-- 顶部轮播图。没配图片时整块不渲染，首页直接从分类开始 -->
    <HomeBanner />

    <div
      v-if="!shop.config.allow_order"
      class="flex items-start gap-2.5 px-5 py-4 rounded-2xl bg-[#e8b86d]/15 text-[#8f7243]"
    >
      <Megaphone class="w-4 h-4 mt-0.5 shrink-0" />
      <p class="text-sm leading-relaxed">
        商城当前暂停下单，您仍可浏览商品与查询已有订单。
      </p>
    </div>

    <!-- 分类 -->
    <nav v-if="shop.categories.length" class="flex flex-wrap gap-2">
      <button
        class="px-4 py-2 rounded-xl text-sm font-medium transition-all duration-200"
        :class="
          !categoryId
            ? 'bg-[#4a9d9a] text-white shadow-md shadow-[#4a9d9a]/20'
            : 'bg-white text-gray-500 shadow-sm shadow-black/[0.03] hover:text-[#4a9d9a] hover:shadow-md'
        "
        @click="setQuery({ category_id: undefined, page: undefined })"
      >
        全部
      </button>
      <button
        v-for="c in shop.categories"
        :key="c.id"
        class="px-4 py-2 rounded-xl text-sm font-medium transition-all duration-200"
        :class="
          categoryId === c.id
            ? 'bg-[#4a9d9a] text-white shadow-md shadow-[#4a9d9a]/20'
            : 'bg-white text-gray-500 shadow-sm shadow-black/[0.03] hover:text-[#4a9d9a] hover:shadow-md'
        "
        @click="setQuery({ category_id: c.id, page: undefined })"
      >
        {{ c.name }}
        <span
          v-if="c.product_count"
          class="ml-1 text-xs"
          :class="categoryId === c.id ? 'text-white/70' : 'text-gray-500'"
        >
          {{ c.product_count }}
        </span>
      </button>
    </nav>

    <!-- 推荐 -->
    <section v-if="recommends.length" class="space-y-4">
      <div class="flex items-center gap-2">
        <Sparkles class="w-4 h-4 text-[#e8b86d]" />
        <h2 class="text-lg font-semibold text-gray-800">推荐商品</h2>
      </div>
      <div class="grid grid-cols-2 lg:grid-cols-4 gap-4 sm:gap-5">
        <ProductCard v-for="p in recommends" :key="`r${p.id}`" :product="p" />
      </div>
    </section>

    <!-- 商品列表 -->
    <section class="space-y-4">
      <header class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div class="flex items-baseline gap-2.5">
          <h2 class="text-lg font-semibold text-gray-800">{{ listTitle }}</h2>
          <span v-if="!loading" class="text-xs text-gray-500 tabular">共 {{ total }} 件</span>
        </div>

        <WdTabs
          :model-value="sort"
          :tabs="sortOptions"
          @update:model-value="(v) => setQuery({ sort: v, page: undefined })"
        />
      </header>

      <div v-if="loading" class="grid grid-cols-2 lg:grid-cols-4 gap-4 sm:gap-5">
        <div
          v-for="i in 8"
          :key="i"
          class="rounded-2xl bg-white shadow-xl shadow-black/[0.04] animate-pulse"
          style="aspect-ratio: 3 / 4.2"
        />
      </div>

      <div
        v-else-if="!products.length"
        class="py-20 flex flex-col items-center text-center bg-white rounded-2xl shadow-xl shadow-black/[0.04]"
      >
        <span class="w-14 h-14 rounded-2xl bg-[#faf8f5] grid place-items-center">
          <Inbox class="w-6 h-6 text-gray-300" />
        </span>
        <p class="mt-4 text-sm text-gray-500">
          {{ keyword ? '没有找到匹配的商品' : '暂无商品' }}
        </p>
        <WdButton
          v-if="keyword || categoryId"
          class="mt-5"
          variant="primary"
          size="sm"
          @click="router.push({ name: 'home' })"
        >
          查看全部商品
        </WdButton>
      </div>

      <template v-else>
        <div class="grid grid-cols-2 lg:grid-cols-4 gap-4 sm:gap-5">
          <ProductCard v-for="p in products" :key="p.id" :product="p" />
        </div>

        <div v-if="totalPages > 1" class="pt-4 flex items-center justify-center gap-4">
          <WdButton :disabled="page <= 1" @click="setQuery({ page: page - 1 })">
            <ChevronLeft class="w-4 h-4" />
            上一页
          </WdButton>
          <span class="text-sm text-gray-500 tabular">{{ page }} / {{ totalPages }}</span>
          <WdButton :disabled="page >= totalPages" @click="setQuery({ page: page + 1 })">
            下一页
            <ChevronRight class="w-4 h-4" />
          </WdButton>
        </div>
      </template>
    </section>
  </div>
</template>
