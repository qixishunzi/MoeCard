<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  Activity,
  AlertCircle,
  ArrowRight,
  KeyRound,
  Package,
  TrendingDown,
  TrendingUp,
  Wallet,
} from 'lucide-vue-next'
import { ApiError, adminApi } from '@/api'
import type { DashboardStats, TrendPoint } from '@/api/types'
import { formatAmount } from '@/utils/format'
import { useShopStore } from '@/stores/shop'
import { WdBadge, WdCard, WdTabs, toast } from '@/ui'

const router = useRouter()
const shop = useShopStore()

const stats = ref<DashboardStats | null>(null)
const trend = ref<TrendPoint[]>([])
const trendDays = ref('7')
const loading = ref(true)
const hovered = ref<number | null>(null)

const symbol = computed(() => shop.symbol())

async function load() {
  loading.value = true
  try {
    const [s, t] = await Promise.all([
      adminApi.dashboard(),
      adminApi.trend(Number(trendDays.value)),
    ])
    stats.value = s
    trend.value = t
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
}

async function switchTrend(v: string) {
  try {
    trend.value = await adminApi.trend(Number(v))
  } catch {
    /* 忽略 */
  }
}

/** 今日 vs 昨日的环比。昨日为 0 时不显示百分比，避免出现 Infinity% */
const dayOverDay = computed(() => {
  const s = stats.value
  if (!s || s.yesterday_revenue === 0) return null
  const rate = ((s.today_revenue - s.yesterday_revenue) / s.yesterday_revenue) * 100
  return { rate: Math.round(rate * 10) / 10, up: rate >= 0 }
})

const cards = computed(() => {
  const s = stats.value
  if (!s) return []
  return [
    {
      label: '今日成交额',
      value: `${symbol.value}${formatAmount(s.today_revenue)}`,
      extra: `今日订单 ${s.today_orders}`,
      color: 'bg-[#4a9d9a]',
      icon: Wallet,
    },
    {
      label: '昨日成交额',
      value: `${symbol.value}${formatAmount(s.yesterday_revenue)}`,
      extra: `近 30 天 ${symbol.value}${formatAmount(s.month_revenue)}`,
      color: 'bg-[#e8b86d]',
      icon: Activity,
    },
    {
      label: '累计成交额',
      value: `${symbol.value}${formatAmount(s.total_revenue)}`,
      extra: `累计订单 ${s.total_orders}`,
      color: 'bg-[#6b8e8e]',
      icon: TrendingUp,
    },
    {
      label: '卡密库存',
      value: String(s.code_stock),
      extra: `在售商品 ${s.on_sale_count} / ${s.product_count}`,
      color: 'bg-[#c17767]',
      icon: KeyRound,
    },
  ]
})

const statusCards = computed(() => {
  const s = stats.value
  if (!s) return []
  return [
    {
      label: '待支付',
      value: s.pending_orders,
      status: 'pending',
      tone: s.pending_orders ? 'text-[#8f7243]' : 'text-gray-700',
    },
    {
      label: '已支付',
      value: s.paid_orders,
      status: 'paid',
      tone: 'text-gray-700',
    },
    {
      label: '待发货',
      value: s.waiting_delivery,
      status: 'waiting_delivery',
      tone: s.waiting_delivery ? 'text-[#c17767]' : 'text-gray-700',
    },
    {
      label: '已完成',
      value: s.completed_orders,
      status: 'completed',
      tone: 'text-[#4a9d9a]',
    },
  ]
})

/**
 * 手写 SVG 折线图 —— 为了这一个图引入图表库（动辄 100KB+）不划算。
 *
 * 用固定的 viewBox 坐标系 + preserveAspectRatio="none" 让它自适应宽度，
 * 点位坐标只需按比例算一次。
 */
const VB_W = 600
const VB_H = 200
const PAD_T = 12
const PAD_B = 12

const chartMax = computed(() => Math.max(1, ...trend.value.map((p) => p.revenue)))

/** 每个数据点在 viewBox 坐标系里的位置 */
const points = computed(() =>
  trend.value.map((p, i) => {
    const n = trend.value.length
    // 只有一个点时放中间，否则均分整个宽度
    const x = n <= 1 ? VB_W / 2 : (i / (n - 1)) * VB_W
    const usable = VB_H - PAD_T - PAD_B
    const y = VB_H - PAD_B - (p.revenue / chartMax.value) * usable
    return { x, y, p, i }
  }),
)

/** 折线路径 */
const linePath = computed(() =>
  points.value
    .map((pt, i) => `${i === 0 ? 'M' : 'L'}${pt.x.toFixed(1)},${pt.y.toFixed(1)}`)
    .join(' '),
)

/** 折线下方的填充区域：闭合到底边 */
const areaPath = computed(() => {
  if (!points.value.length) return ''
  const first = points.value[0]
  const last = points.value[points.value.length - 1]
  return `M${first.x.toFixed(1)},${VB_H} ${linePath.value.slice(1)} L${last.x.toFixed(1)},${VB_H} Z`
})

/** 横轴标签：点太多时抽稀，避免文字叠在一起 */
const xLabels = computed(() => {
  const n = trend.value.length
  if (n === 0) return []
  const step = n <= 8 ? 1 : Math.ceil(n / 6)
  return points.value.filter((pt) => pt.i % step === 0 || pt.i === n - 1)
})

/** 纵轴刻度：0 / 一半 / 最大 */
const yTicks = computed(() => {
  const usable = VB_H - PAD_T - PAD_B
  return [0, 0.5, 1].map((r) => ({
    y: VB_H - PAD_B - r * usable,
    value: chartMax.value * r,
  }))
})

const statusTone: Record<string, 'teal' | 'amber' | 'clay' | 'slate' | 'gray'> = {
  completed: 'teal',
  paid: 'slate',
  waiting_delivery: 'clay',
  pending: 'amber',
  paying: 'amber',
  refunded: 'clay',
  cancelled: 'gray',
  expired: 'gray',
}

onMounted(load)
</script>

<template>
  <div v-if="loading && !stats" class="py-32 flex justify-center">
    <svg class="w-7 h-7 animate-spin text-[#4a9d9a]" viewBox="0 0 24 24" fill="none">
      <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="2.5" opacity="0.2" />
      <path
        d="M21 12a9 9 0 0 0-9-9"
        stroke="currentColor"
        stroke-width="2.5"
        stroke-linecap="round"
      />
    </svg>
  </div>

  <div v-else-if="stats" class="space-y-6">
    <!-- 异常订单告警 -->
    <button
      v-if="stats.needs_attention > 0"
      class="w-full flex items-start gap-3.5 text-left bg-white rounded-2xl shadow-xl shadow-black/[0.04] p-5 hover:shadow-2xl hover:-translate-y-0.5 transition-all duration-300"
      @click="router.push({ name: 'admin-orders', query: { attention: '1' } })"
    >
      <span class="w-9 h-9 rounded-xl bg-[#c17767]/10 grid place-items-center shrink-0">
        <AlertCircle class="w-4 h-4 text-[#c17767]" />
      </span>
      <div class="flex-1 min-w-0">
        <p class="text-sm font-medium text-gray-800">
          有 {{ stats.needs_attention }} 个订单需要人工处理
        </p>
        <p class="mt-0.5 text-xs text-gray-400 leading-relaxed">
          这些订单已收到付款但发货异常（如卡密不足、迟到的支付回调）
        </p>
      </div>
      <ArrowRight class="w-4 h-4 text-[#c17767] mt-2.5 shrink-0" />
    </button>

    <!-- 核心指标 -->
    <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-5">
      <div
        v-for="c in cards"
        :key="c.label"
        class="bg-white rounded-2xl shadow-xl shadow-black/[0.04] p-6 hover:shadow-2xl hover:-translate-y-1 transition-all duration-300"
      >
        <div class="flex items-center justify-between mb-4">
          <span class="text-sm text-gray-400">{{ c.label }}</span>
          <span class="w-9 h-9 rounded-xl grid place-items-center shrink-0" :class="c.color">
            <component :is="c.icon" class="w-4 h-4 text-white" />
          </span>
        </div>
        <div class="text-2xl font-semibold text-gray-800 mb-1 tabular truncate">
          {{ c.value }}
        </div>
        <div class="flex items-center gap-1.5 text-xs">
          <template v-if="c.label === '今日成交额' && dayOverDay">
            <component
              :is="dayOverDay.up ? TrendingUp : TrendingDown"
              class="w-3.5 h-3.5"
              :class="dayOverDay.up ? 'text-[#4a9d9a]' : 'text-[#c17767]'"
            />
            <span class="font-medium" :class="dayOverDay.up ? 'text-[#4a9d9a]' : 'text-[#c17767]'">
              {{ Math.abs(dayOverDay.rate) }}%
            </span>
            <span class="text-gray-400">较昨日 · {{ c.extra }}</span>
          </template>
          <span v-else class="text-gray-400">{{ c.extra }}</span>
        </div>
      </div>
    </div>

    <!-- 订单状态 -->
    <div class="grid grid-cols-2 lg:grid-cols-4 gap-5">
      <button
        v-for="s in statusCards"
        :key="s.label"
        class="bg-white rounded-2xl shadow-xl shadow-black/[0.04] p-5 flex flex-col items-center gap-1 hover:shadow-2xl hover:-translate-y-1 transition-all duration-300"
        @click="router.push({ name: 'admin-orders', query: { status: s.status } })"
      >
        <span class="text-2xl font-semibold tabular" :class="s.tone">{{ s.value }}</span>
        <span class="text-xs text-gray-400">{{ s.label }}</span>
      </button>
    </div>

    <!-- 趋势 -->
    <WdCard title="销售趋势" subtitle="按商城时区统计">
      <template #actions>
        <WdTabs
          v-model="trendDays"
          :tabs="[
            { value: '7', label: '近 7 天' },
            { value: '30', label: '近 30 天' },
          ]"
          on="card"
          @change="switchTrend"
        />
      </template>

      <div v-if="!trend.length" class="py-12 text-center text-sm text-gray-500">暂无数据</div>

      <div v-else class="relative">
        <!-- 悬停提示。绝对定位在图上方，跟随当前点的横向位置 -->
        <div
          v-if="hovered !== null && points[hovered]"
          class="absolute z-10 -translate-x-1/2 -translate-y-full bg-gray-800 text-white text-xs px-2.5 py-1.5 rounded-lg whitespace-nowrap pointer-events-none"
          :style="{
            left: `${(points[hovered].x / VB_W) * 100}%`,
            top: `${(points[hovered].y / VB_H) * 100}%`,
            marginTop: '-8px',
          }"
        >
          <div class="tabular">{{ symbol }}{{ formatAmount(points[hovered].p.revenue) }}</div>
          <div class="text-gray-300 tabular">
            {{ points[hovered].p.orders }} 单 ·
            {{ points[hovered].p.date.slice(5) }}
          </div>
        </div>

        <svg
          :viewBox="`0 0 ${VB_W} ${VB_H}`"
          preserveAspectRatio="none"
          class="w-full h-52 overflow-visible"
          role="img"
          aria-label="销售趋势折线图"
          @mouseleave="hovered = null"
        >
          <defs>
            <linearGradient id="trendFill" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stop-color="#4a9d9a" stop-opacity="0.22" />
              <stop offset="100%" stop-color="#4a9d9a" stop-opacity="0" />
            </linearGradient>
          </defs>

          <!-- 网格线 -->
          <line
            v-for="t in yTicks"
            :key="`g${t.y}`"
            x1="0"
            :y1="t.y"
            :x2="VB_W"
            :y2="t.y"
            stroke="#f0efec"
            stroke-width="1"
            vector-effect="non-scaling-stroke"
          />

          <path :d="areaPath" fill="url(#trendFill)" />
          <path
            :d="linePath"
            fill="none"
            stroke="#4a9d9a"
            stroke-width="2"
            stroke-linejoin="round"
            stroke-linecap="round"
            vector-effect="non-scaling-stroke"
          />

          <!--
            平时不画数据点，只在悬停时高亮当前那一个。
            原来 7 天视图会给每天都点一个圆，30 天视图因为点太密而隐藏，
            于是切换范围时图的观感完全变了两副样子；统一成干净的折线。
            只有一个点时例外 —— 不画的话整张图上什么都看不见。
          -->
          <circle
            v-for="pt in points"
            :key="`p${pt.i}`"
            :cx="pt.x"
            :cy="pt.y"
            :r="hovered === pt.i ? 5 : points.length === 1 ? 3 : 0"
            :fill="hovered === pt.i ? '#e8b86d' : '#4a9d9a'"
            vector-effect="non-scaling-stroke"
          />

          <!-- 透明热区：让整条竖带都能触发悬停，而不用精准点中那个小圆点 -->
          <rect
            v-for="pt in points"
            :key="`h${pt.i}`"
            :x="pt.x - VB_W / Math.max(1, trend.length) / 2"
            y="0"
            :width="VB_W / Math.max(1, trend.length)"
            :height="VB_H"
            fill="transparent"
            @mouseenter="hovered = pt.i"
          />
        </svg>

        <!-- 横轴：放在 svg 外面用普通元素排版，避免 preserveAspectRatio 把文字拉变形 -->
        <div class="relative h-4 mt-1">
          <span
            v-for="pt in xLabels"
            :key="`x${pt.i}`"
            class="absolute -translate-x-1/2 text-[10px] text-gray-500 tabular whitespace-nowrap"
            :style="{ left: `${(pt.x / VB_W) * 100}%` }"
          >
            {{ pt.p.date.slice(5) }}
          </span>
        </div>

        <p class="mt-3 text-xs text-gray-500">
          纵轴最高 {{ symbol }}{{ formatAmount(chartMax) }} · 统计从系统部署当天开始
        </p>
      </div>
    </WdCard>

    <!-- 最近订单 -->
    <WdCard title="最近订单" flush>
      <template #actions>
        <button
          class="flex items-center gap-1 text-xs font-medium text-[#4a9d9a] hover:underline"
          @click="router.push({ name: 'admin-orders' })"
        >
          查看全部
          <ArrowRight class="w-3 h-3" />
        </button>
      </template>

      <div class="px-6 pb-6 pt-4">
        <div v-if="!stats.recent_orders.length" class="py-12 text-center">
          <span class="w-12 h-12 mx-auto rounded-2xl bg-[#faf8f5] grid place-items-center">
            <Package class="w-5 h-5 text-gray-300" />
          </span>
          <p class="mt-3 text-sm text-gray-400">还没有订单</p>
        </div>

        <div v-else class="overflow-x-auto -mx-6 px-6">
          <table class="w-full min-w-max">
            <thead>
              <tr class="border-b border-gray-100">
                <th class="text-left py-3 px-4 text-xs font-medium text-gray-400 tracking-wider">
                  订单号
                </th>
                <th class="text-left py-3 px-4 text-xs font-medium text-gray-400 tracking-wider">
                  商品
                </th>
                <th class="text-left py-3 px-4 text-xs font-medium text-gray-400 tracking-wider">
                  买家
                </th>
                <th class="text-right py-3 px-4 text-xs font-medium text-gray-400 tracking-wider">
                  金额
                </th>
                <th class="text-center py-3 px-4 text-xs font-medium text-gray-400 tracking-wider">
                  状态
                </th>
                <th class="text-left py-3 px-4 text-xs font-medium text-gray-400 tracking-wider">
                  时间
                </th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="o in stats.recent_orders"
                :key="o.id"
                class="border-b border-gray-50 hover:bg-[#faf8f5] transition-colors duration-200 cursor-pointer"
                @click="
                  router.push({
                    name: 'admin-orders',
                    query: { keyword: o.order_no },
                  })
                "
              >
                <td class="py-3.5 px-4">
                  <span class="font-mono text-xs text-[#4a9d9a]">{{ o.order_no }}</span>
                </td>
                <td class="py-3.5 px-4 text-sm text-gray-600 max-w-52 truncate">
                  {{ o.product_name || '—' }}
                  <span v-if="o.quantity > 1" class="text-gray-400">× {{ o.quantity }}</span>
                </td>
                <td class="py-3.5 px-4 text-sm text-gray-500">{{ o.email }}</td>
                <td class="py-3.5 px-4 text-sm text-gray-700 text-right tabular">
                  {{ symbol }}{{ formatAmount(o.pay_amount) }}
                </td>
                <td class="py-3.5 px-4 text-center">
                  <WdBadge :tone="statusTone[o.status] ?? 'gray'">{{ o.status_text }}</WdBadge>
                </td>
                <td class="py-3.5 px-4 text-xs text-gray-400 tabular">
                  {{ o.created_at }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </WdCard>
  </div>
</template>
