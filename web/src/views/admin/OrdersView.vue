<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { AlertCircle, Copy, Download, Search } from 'lucide-vue-next'
import { ApiError, adminApi } from '@/api'
import type { Order } from '@/api/types'
import { ORDER_STATUS_LABELS, copyText, formatAmount, parseAmount } from '@/utils/format'
import { useShopStore } from '@/stores/shop'
import {
  WdBadge,
  WdButton,
  WdCard,
  WdInput,
  WdModal,
  WdPagination,
  WdTable,
  confirmDialog,
  toast,
  type Column,
} from '@/ui'

const route = useRoute()
const shop = useShopStore()

const list = ref<Order[]>([])
const total = ref(0)
const loading = ref(false)

const query = reactive({
  keyword: '',
  email: '',
  product: '',
  status: '',
  provider: '',
  attention: '',
  start_at: '',
  end_at: '',
  page: 1,
  page_size: 20,
})

const detailVisible = ref(false)
/**
 * 买家下单时填写的自定义信息。
 *
 * 后端已经把字段 key 配上了商品里定义的中文标签（account → 接收账号），
 * 前端直接用即可 —— 显示成裸 key 的话，管理员根本对不上是哪一项。
 */
const customEntries = computed(() => detail.value?.custom_data ?? [])

async function copyValue(v: string, label: string) {
  const ok = await copyText(v)
  ok ? toast.success(`${label}已复制`) : toast.error('复制失败')
}

const exporting = ref(false)

/** 按当前筛选条件导出。带上筛选很重要 —— 对账通常只要某个时间段。 */
async function exportOrders() {
  exporting.value = true
  try {
    await adminApi.download(
      adminApi.exportOrdersURL({
        status: query.status || undefined,
        keyword: query.keyword || undefined,
        provider: query.provider || undefined,
      }),
      `订单-${new Date().toISOString().slice(0, 10)}.csv`,
    )
    toast.success('导出完成')
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '导出失败')
  } finally {
    exporting.value = false
  }
}

const detail = ref<Order | null>(null)
const detailLoading = ref(false)

const deliverVisible = ref(false)
const deliverContent = ref('')
const delivering = ref(false)

const remarkVisible = ref(false)
const remarkText = ref('')

const refundVisible = ref(false)
const refundForm = reactive({ amountYuan: 0 as number | null, reason: '', manual: true })
const refunding = ref(false)

const symbol = computed(() => shop.symbol())

const statusOptions = Object.entries(ORDER_STATUS_LABELS).map(([value, label]) => ({ label, value }))

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

const columns: Column[] = [
  { key: 'order_no', label: '订单号', width: '215px' },
  { key: 'product', label: '商品' },
  { key: 'email', label: '买家', width: '160px', hideOnMobile: true },
  { key: 'pay_amount', label: '金额', width: '115px', align: 'right' },
  { key: 'status', label: '状态', width: '100px', align: 'center' },
  { key: 'payment_method', label: '支付方式', width: '120px', hideOnMobile: true },
  { key: 'created_at', label: '创建时间', width: '160px', hideOnMobile: true },
  { key: 'actions', label: '操作', width: '150px', align: 'center' },
]

async function load() {
  loading.value = true
  try {
    const res = await adminApi.orders({
      keyword: query.keyword || undefined,
      email: query.email || undefined,
      product: query.product || undefined,
      status: query.status || undefined,
      provider: query.provider || undefined,
      attention: query.attention || undefined,
      start_at: query.start_at || undefined,
      end_at: query.end_at || undefined,
      page: query.page,
      page_size: query.page_size,
    })
    list.value = res.list ?? []
    total.value = res.total
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
}

function search() {
  query.page = 1
  load()
}

function resetQuery() {
  Object.assign(query, {
    keyword: '',
    email: '',
    product: '',
    status: '',
    provider: '',
    attention: '',
    start_at: '',
    end_at: '',
    page: 1,
  })
  load()
}

async function openDetail(row: Order) {
  detailVisible.value = true
  detailLoading.value = true
  detail.value = null
  try {
    detail.value = await adminApi.order(row.id!)
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '加载详情失败')
  } finally {
    detailLoading.value = false
  }
}

/**
 * 打开发货弹窗。
 *
 * 必须重新拉一次详情，不能直接用列表行：列表里的邮箱是脱敏的，
 * 而且不含买家填写的信息（游戏账号、大区等）——
 * 手动发货恰恰就是靠那些信息才知道该把东西发给谁。
 */
async function openDeliver(order: Order) {
  deliverContent.value = ''
  deliverVisible.value = true
  detail.value = order
  detailLoading.value = true
  try {
    detail.value = await adminApi.order(order.id!)
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '加载订单信息失败')
  } finally {
    detailLoading.value = false
  }
}

async function submitDeliver() {
  if (!detail.value || !deliverContent.value.trim()) {
    toast.error('请填写发货内容')
    return
  }
  delivering.value = true
  try {
    const updated = await adminApi.deliverOrder(detail.value.id!, deliverContent.value)
    toast.success('发货成功，已通知买家')
    deliverVisible.value = false
    detail.value = updated
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '发货失败')
  } finally {
    delivering.value = false
  }
}

function openRemark(order: Order) {
  detail.value = order
  remarkText.value = order.remark ?? ''
  remarkVisible.value = true
}

async function submitRemark() {
  if (!detail.value) return
  try {
    await adminApi.remarkOrder(detail.value.id!, remarkText.value)
    toast.success('备注已保存')
    remarkVisible.value = false
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '保存失败')
  }
}

function openRefund(order: Order) {
  detail.value = order
  refundForm.amountYuan = order.pay_amount / 100
  refundForm.reason = ''
  refundForm.manual = true
  refundVisible.value = true
}

async function submitRefund() {
  if (!detail.value) return
  const amount = parseAmount(refundForm.amountYuan ?? 0)
  if (amount <= 0 || amount > detail.value.pay_amount) {
    toast.error(`退款金额需在 0 - ${formatAmount(detail.value.pay_amount)} 之间`)
    return
  }
  refunding.value = true
  try {
    await adminApi.refundOrder(detail.value.id!, {
      amount,
      reason: refundForm.reason,
      manual: refundForm.manual,
    })
    toast.success('退款已处理')
    refundVisible.value = false
    detailVisible.value = false
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '退款失败')
  } finally {
    refunding.value = false
  }
}

async function resendMail(order: Order) {
  try {
    await adminApi.resendMail(order.id!)
    toast.success('邮件已发送')
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '发送失败')
  }
}

async function clearAttention(order: Order) {
  const ok = await confirmDialog({ message: '确认该异常已处理完毕？', confirmText: '标记已处理' })
  if (!ok) return
  try {
    await adminApi.clearAttention(order.id!)
    toast.success('已清除')
    detailVisible.value = false
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '操作失败')
  }
}

async function copy(text: string) {
  const ok = await copyText(text)
  ok ? toast.success('已复制') : toast.error('复制失败')
}

const detailDelivery = computed(() => {
  const o = detail.value
  if (!o) return ''
  const parts = o.items.map((i) => i.delivery_content).filter((c): c is string => !!c?.trim())
  return parts.length ? parts.join('\n') : (o.delivery_content ?? '')
})

// 支持从 Dashboard 带查询参数跳过来
watch(
  () => route.query,
  (q) => {
    if (q.status) query.status = String(q.status)
    if (q.keyword) query.keyword = String(q.keyword)
    if (q.attention) query.attention = String(q.attention)
    load()
  },
)

onMounted(() => {
  if (route.query.status) query.status = String(route.query.status)
  if (route.query.keyword) query.keyword = String(route.query.keyword)
  if (route.query.attention) query.attention = String(route.query.attention)
  load()
})
</script>

<template>
  <div class="space-y-5">
    <WdCard flush>
      <div class="px-6 py-5">
        <!-- 筛选 -->
        <div class="flex flex-wrap items-end gap-3 mb-5">
          <div class="relative w-52">
            <Search class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-300 z-10" />
            <input
              v-model="query.keyword"
              placeholder="订单号 / 支付流水号"
              aria-label="搜索订单号或支付流水号"
              class="w-full pl-9 pr-3.5 py-2.5 bg-white border border-gray-200 rounded-xl text-sm text-gray-800 placeholder:text-gray-300 focus:outline-none focus:ring-2 focus:ring-[#4a9d9a]/30 focus:border-[#4a9d9a] transition-all duration-200"
              @keyup.enter="search"
            />
          </div>
          <WdInput
            v-model="query.email"
            placeholder="买家邮箱（完整匹配）"
            class="w-48"
            @enter="search"
          />
          <WdInput v-model="query.product" placeholder="商品名称" class="w-36" @enter="search" />
          <WdInput
            v-model="query.status"
            type="select"
            placeholder="全部状态"
            clearable
            class="w-32"
            :options="statusOptions"
            @change="search"
          />
          <WdInput
            v-model="query.provider"
            type="select"
            placeholder="支付方式"
            clearable
            class="w-36"
            :options="[
              { label: '易支付 V1', value: 'yipay_v1' },
              { label: '易支付 V2', value: 'yipay_v2' },
              { label: '支付宝', value: 'alipay' },
              { label: '微信支付', value: 'wechat' },
              { label: 'Stripe', value: 'stripe' },
              { label: 'HashPay', value: 'hashpay' },
            ]"
            @change="search"
          />
          <!-- 日期框没有可见标题，得自己给可访问名，否则读屏只会念"日期输入框" -->
          <WdInput
            v-model="query.start_at"
            type="date"
            class="w-40"
            aria-label="起始日期"
            @change="search"
          />
          <WdInput
            v-model="query.end_at"
            type="date"
            class="w-40"
            aria-label="截止日期"
            @change="search"
          />

          <label class="flex items-center gap-2 h-[42px] text-sm text-gray-600 cursor-pointer">
            <input
              type="checkbox"
              class="accent-[#c17767]"
              :checked="query.attention === '1'"
              @change="
                query.attention = ($event.target as HTMLInputElement).checked ? '1' : '';
                search()
              "
            />
            只看异常订单
          </label>

          <WdButton variant="primary" @click="search">查询</WdButton>
          <WdButton @click="resetQuery">重置</WdButton>
          <WdButton :loading="exporting" @click="exportOrders">
            <Download class="w-4 h-4" />
            导出 CSV
          </WdButton>
        </div>

        <WdTable :columns="columns" :rows="list" :loading="loading" empty-text="没有符合条件的订单">
          <template #order_no="{ row }">
            <div class="flex items-center gap-2">
              <button
                class="font-mono text-xs text-[#4a9d9a] hover:underline"
                @click="openDetail(row as Order)"
              >
                {{ row.order_no }}
              </button>
              <WdBadge v-if="row.needs_attention" tone="clay" dot>异常</WdBadge>
            </div>
          </template>

          <template #product="{ row }">
            <span class="text-sm text-gray-600 line-clamp-1 max-w-48">
              {{ row.items?.[0]?.product_name || '—' }}
              <span v-if="row.items?.[0]" class="text-gray-400">× {{ row.items[0].quantity }}</span>
            </span>
          </template>

          <template #email="{ row }">
            <span class="text-sm text-gray-500">{{ row.email }}</span>
          </template>

          <template #pay_amount="{ row }">
            <p class="text-sm font-medium text-gray-800 tabular">
              {{ symbol }}{{ formatAmount(row.pay_amount) }}
            </p>
            <p v-if="row.discount_amount > 0" class="text-[11px] text-[#4a9d9a] tabular">
              省 {{ symbol }}{{ formatAmount(row.discount_amount) }}
            </p>
          </template>

          <template #status="{ row }">
            <WdBadge :tone="statusTone[row.status] ?? 'gray'">{{ row.status_text }}</WdBadge>
          </template>

          <template #payment_method="{ row }">
            <span class="text-sm text-gray-500">{{ row.payment_method || '—' }}</span>
          </template>

          <template #created_at="{ row }">
            <span class="text-xs text-gray-400 tabular">{{ row.created_at }}</span>
          </template>

          <template #actions="{ row }">
            <div class="flex items-center justify-center gap-3">
              <button
                class="text-xs font-medium text-[#4a9d9a] hover:underline"
                @click="openDetail(row as Order)"
              >
                详情
              </button>
              <button
                v-if="row.status === 'waiting_delivery' || row.status === 'paid'"
                class="text-xs font-medium text-[#c17767] hover:underline"
                @click="openDeliver(row as Order)"
              >
                发货
              </button>
              <button
                class="text-xs font-medium text-gray-400 hover:text-gray-600 hover:underline"
                @click="openRemark(row as Order)"
              >
                备注
              </button>
            </div>
          </template>
        </WdTable>

        <WdPagination
          v-model:page="query.page"
          v-model:page-size="query.page_size"
          :total="total"
          @change="load"
        />
      </div>
    </WdCard>

    <!-- 订单详情抽屉 -->
    <WdModal v-model="detailVisible" title="订单详情" side="right" width="620px">
      <div v-if="detailLoading" class="py-20 flex justify-center">
        <svg class="w-6 h-6 animate-spin text-[#4a9d9a]" viewBox="0 0 24 24" fill="none">
          <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="2.5" opacity="0.2" />
          <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" />
        </svg>
      </div>

      <div v-else-if="detail" class="space-y-6">
        <!-- 异常告警 -->
        <div v-if="detail.needs_attention" class="flex items-start gap-3 p-4 rounded-xl bg-[#c17767]/[0.07]">
          <AlertCircle class="w-4 h-4 text-[#c17767] mt-0.5 shrink-0" />
          <div class="flex-1 min-w-0">
            <p class="text-sm font-medium text-[#c17767]">该订单需要人工处理</p>
            <p class="mt-1 text-xs text-gray-600 leading-relaxed whitespace-pre-line">
              {{ detail.attention_reason }}
            </p>
            <WdButton size="sm" class="mt-3" @click="clearAttention(detail)">标记为已处理</WdButton>
          </div>
        </div>

        <dl class="space-y-3 text-sm">
          <div class="flex gap-4">
            <dt class="w-20 shrink-0 text-gray-400">订单号</dt>
            <dd class="flex items-center gap-2 min-w-0">
              <span class="font-mono text-xs text-gray-700 break-all">{{ detail.order_no }}</span>
              <button
                class="shrink-0 text-xs text-[#4a9d9a] hover:underline"
                @click="copy(detail.order_no)"
              >
                复制
              </button>
            </dd>
          </div>
          <div class="flex gap-4">
            <dt class="w-20 shrink-0 text-gray-400">状态</dt>
            <dd><WdBadge :tone="statusTone[detail.status] ?? 'gray'">{{ detail.status_text }}</WdBadge></dd>
          </div>
          <div class="flex gap-4">
            <dt class="w-20 shrink-0 text-gray-400">买家邮箱</dt>
            <dd class="flex items-center gap-2 min-w-0">
              <span class="text-gray-700 break-all">{{ detail.email }}</span>
              <button
                class="shrink-0 text-xs text-[#4a9d9a] hover:underline"
                @click="copy(detail.email)"
              >
                复制
              </button>
            </dd>
          </div>
          <div class="flex gap-4">
            <dt class="w-20 shrink-0 text-gray-400">商品金额</dt>
            <dd class="text-gray-700 tabular">
              {{ symbol }}{{ formatAmount(detail.original_amount) }}
            </dd>
          </div>
          <div v-if="detail.discount_amount > 0" class="flex gap-4">
            <dt class="w-20 shrink-0 text-gray-400">优惠券</dt>
            <dd class="text-[#4a9d9a] tabular">
              {{ detail.coupon_code }} · 抵扣 {{ symbol }}{{ formatAmount(detail.discount_amount) }}
            </dd>
          </div>
          <div class="flex gap-4">
            <dt class="w-20 shrink-0 text-gray-400">实付金额</dt>
            <dd class="text-base font-semibold text-gray-800 tabular">
              {{ symbol }}{{ formatAmount(detail.pay_amount) }}
            </dd>
          </div>
          <div v-if="detail.refund_amount > 0" class="flex gap-4">
            <dt class="w-20 shrink-0 text-gray-400">退款</dt>
            <dd class="text-[#c17767]">
              <span class="tabular">{{ symbol }}{{ formatAmount(detail.refund_amount) }}</span>
              <span v-if="detail.refund_reason" class="text-gray-400"> · {{ detail.refund_reason }}</span>
              <p v-if="detail.refunded_at" class="text-xs text-gray-400 tabular">{{ detail.refunded_at }}</p>
            </dd>
          </div>
          <div class="flex gap-4">
            <dt class="w-20 shrink-0 text-gray-400">发货方式</dt>
            <dd class="text-gray-700">
              {{ detail.delivery_type === 'auto' ? '自动发货' : '手动发货' }}
            </dd>
          </div>
          <div class="flex gap-4">
            <dt class="w-20 shrink-0 text-gray-400">支付方式</dt>
            <dd class="text-gray-700">
              {{ detail.payment_method || '—' }}
              <span v-if="detail.payment_provider" class="text-gray-400">
                ({{ detail.payment_provider }})
              </span>
            </dd>
          </div>
          <div v-if="detail.trade_no" class="flex gap-4">
            <dt class="w-20 shrink-0 text-gray-400">支付流水</dt>
            <dd class="font-mono text-xs text-gray-500 break-all">{{ detail.trade_no }}</dd>
          </div>
          <div class="flex gap-4">
            <dt class="w-20 shrink-0 text-gray-400">创建时间</dt>
            <dd class="text-gray-700 tabular">{{ detail.created_at }}</dd>
          </div>
          <div v-if="detail.paid_at" class="flex gap-4">
            <dt class="w-20 shrink-0 text-gray-400">支付时间</dt>
            <dd class="text-gray-700 tabular">{{ detail.paid_at }}</dd>
          </div>
          <div v-if="detail.delivered_at" class="flex gap-4">
            <dt class="w-20 shrink-0 text-gray-400">发货时间</dt>
            <dd class="text-gray-700 tabular">{{ detail.delivered_at }}</dd>
          </div>
          <div v-if="detail.client_ip" class="flex gap-4">
            <dt class="w-20 shrink-0 text-gray-400">下单 IP</dt>
            <dd class="text-gray-500">{{ detail.client_ip }}</dd>
          </div>
          <div v-if="detail.remark" class="flex gap-4">
            <dt class="w-20 shrink-0 text-gray-400">备注</dt>
            <dd class="text-gray-700 whitespace-pre-line">{{ detail.remark }}</dd>
          </div>
        </dl>

        <!-- 买家填写的信息：代充类商品全靠这个才知道该充给谁 -->
        <div v-if="customEntries.length" class="px-4 py-3.5 rounded-xl bg-[#4a9d9a]/[0.06]">
          <h4 class="text-sm font-semibold text-gray-800 mb-2.5">买家填写的信息</h4>
          <dl class="space-y-2 text-sm">
            <div v-for="f in customEntries" :key="f.key" class="flex gap-4">
              <dt class="w-24 shrink-0 text-gray-500">{{ f.label }}</dt>
              <dd class="flex items-center gap-2 min-w-0 text-gray-800 break-all whitespace-pre-wrap">
                {{ f.value }}
                <button
                  class="shrink-0 p-1 rounded-lg text-gray-300 hover:text-[#4a9d9a] hover:bg-white transition-all duration-200"
                  :aria-label="`复制${f.label}`"
                  @click="copyValue(f.value, f.label)"
                >
                  <Copy class="w-3.5 h-3.5" />
                </button>
              </dd>
            </div>
          </dl>
        </div>

        <!-- 明细 -->
        <div>
          <h4 class="text-sm font-semibold text-gray-800 mb-3">商品明细</h4>
          <div class="rounded-xl border border-gray-100 overflow-hidden">
            <table class="w-full">
              <thead class="bg-[#faf8f5]">
                <tr>
                  <th class="text-left py-2.5 px-3 text-xs font-medium text-gray-400">商品</th>
                  <th class="text-right py-2.5 px-3 text-xs font-medium text-gray-400">单价</th>
                  <th class="text-center py-2.5 px-3 text-xs font-medium text-gray-400">数量</th>
                  <th class="text-right py-2.5 px-3 text-xs font-medium text-gray-400">小计</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(it, i) in detail.items" :key="i" class="border-t border-gray-50">
                  <td class="py-2.5 px-3 text-sm text-gray-700">{{ it.product_name }}</td>
                  <td class="py-2.5 px-3 text-sm text-gray-500 text-right tabular">
                    {{ symbol }}{{ formatAmount(it.product_price) }}
                  </td>
                  <td class="py-2.5 px-3 text-sm text-gray-500 text-center tabular">
                    {{ it.quantity }}
                  </td>
                  <td class="py-2.5 px-3 text-sm text-gray-700 text-right tabular">
                    {{ symbol }}{{ formatAmount(it.subtotal) }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <!-- 发货内容 -->
        <div v-if="detailDelivery">
          <div class="flex items-center justify-between mb-3">
            <h4 class="text-sm font-semibold text-gray-800">发货内容</h4>
            <button
              class="text-xs font-medium text-[#4a9d9a] hover:underline"
              @click="copy(detailDelivery)"
            >
              复制
            </button>
          </div>
          <pre
            class="p-4 rounded-xl bg-[#faf8f5] border border-gray-100 font-mono text-[13px] leading-relaxed text-gray-700 whitespace-pre-wrap break-all max-h-64 overflow-auto"
            >{{ detailDelivery }}</pre
          >
        </div>

        <div class="flex flex-wrap gap-2.5 pt-4 border-t border-gray-100">
          <WdButton
            v-if="detail.status === 'waiting_delivery' || detail.status === 'paid'"
            variant="warning"
            @click="openDeliver(detail)"
          >
            填写发货内容
          </WdButton>
          <WdButton @click="openRemark(detail)">修改备注</WdButton>
          <WdButton v-if="detail.paid_at" @click="resendMail(detail)">重发邮件</WdButton>
          <WdButton
            v-if="['paid', 'waiting_delivery', 'completed'].includes(detail.status)"
            variant="danger"
            @click="openRefund(detail)"
          >
            退款
          </WdButton>
        </div>
      </div>
    </WdModal>

    <!-- 手动发货 -->
    <WdModal v-model="deliverVisible" title="手动发货" width="560px" :close-on-overlay="false">
      <div class="px-4 py-3 rounded-xl bg-[#faf8f5] text-xs text-gray-500 leading-relaxed">
        发货内容会展示在买家的订单详情页，并通过邮件发送给买家。请仔细核对后再提交。
      </div>
      <dl class="mt-4 space-y-2 text-sm">
        <div class="flex gap-4">
          <dt class="w-16 shrink-0 text-gray-400">订单号</dt>
          <dd class="font-mono text-xs text-gray-700">{{ detail?.order_no }}</dd>
        </div>
        <div class="flex gap-4">
          <dt class="w-16 shrink-0 text-gray-400">买家</dt>
          <dd class="text-gray-700">{{ detail?.email }}</dd>
        </div>
      </dl>

      <!-- 买家填写的信息：手动发货就靠它，必须放在填发货内容之前 -->
      <div
        v-if="customEntries.length"
        class="mt-4 px-4 py-3 rounded-xl bg-[#4a9d9a]/8 border border-[#4a9d9a]/20"
      >
        <p class="text-xs font-medium text-[#3d7f7d] mb-2">买家填写的信息</p>
        <dl class="space-y-1.5 text-sm">
          <div v-for="f in customEntries" :key="f.key" class="flex gap-3">
            <dt class="w-20 shrink-0 text-gray-500">{{ f.label }}</dt>
            <dd class="flex-1 min-w-0 text-gray-800 break-all">
              {{ f.value || '—' }}
              <button
                v-if="f.value"
                class="ml-1.5 text-xs text-[#4a9d9a] hover:underline"
                @click="copyValue(f.value, f.label)"
              >
                复制
              </button>
            </dd>
          </div>
        </dl>
      </div>
      <div class="mt-4">
        <WdInput
          v-model="deliverContent"
          type="textarea"
          label="发货内容"
          required
          :rows="8"
          mono
          placeholder="账号：xxxx&#10;密码：xxxx&#10;备注：请及时修改密码"
        />
      </div>
      <template #footer>
        <WdButton @click="deliverVisible = false">取消</WdButton>
        <WdButton variant="primary" :loading="delivering" @click="submitDeliver">确认发货</WdButton>
      </template>
    </WdModal>

    <!-- 备注 -->
    <WdModal v-model="remarkVisible" title="订单备注" width="460px">
      <WdInput v-model="remarkText" type="textarea" :rows="5" :maxlength="1000" />
      <template #footer>
        <WdButton @click="remarkVisible = false">取消</WdButton>
        <WdButton variant="primary" @click="submitRemark">保存</WdButton>
      </template>
    </WdModal>

    <!-- 退款 -->
    <WdModal v-model="refundVisible" title="订单退款" width="480px" :close-on-overlay="false">
      <div class="px-4 py-3 rounded-xl bg-[#e8b86d]/10 text-xs text-[#b8873f] leading-relaxed">
        <span class="font-medium">人工退款</span>：仅在系统内记账，需要你自行在支付平台后台操作实际退款。<br />
        <span class="font-medium">渠道退款</span>：调用支付渠道接口自动退款（部分渠道不支持，如易支付 V1、HashPay）。
      </div>

      <dl class="mt-4 space-y-2 text-sm">
        <div class="flex gap-4">
          <dt class="w-20 shrink-0 text-gray-400">订单号</dt>
          <dd class="font-mono text-xs text-gray-700">{{ detail?.order_no }}</dd>
        </div>
        <div class="flex gap-4">
          <dt class="w-20 shrink-0 text-gray-400">实付金额</dt>
          <dd class="text-gray-700 tabular">
            {{ symbol }}{{ formatAmount(detail?.pay_amount ?? 0) }}
          </dd>
        </div>
      </dl>

      <div class="mt-4">
        <label class="block mb-1.5 text-xs font-medium text-gray-500">退款方式</label>
        <div class="flex gap-2">
          <button
            v-for="opt in [
              { v: true, t: '人工退款（仅记账）' },
              { v: false, t: '渠道退款' },
            ]"
            :key="String(opt.v)"
            type="button"
            class="flex-1 px-4 py-2.5 rounded-xl text-sm font-medium border transition-all duration-200"
            :class="
              refundForm.manual === opt.v
                ? 'border-[#4a9d9a] bg-[#4a9d9a]/[0.07] text-[#4a9d9a]'
                : 'border-gray-200 text-gray-500 hover:border-gray-300'
            "
            @click="refundForm.manual = opt.v"
          >
            {{ opt.t }}
          </button>
        </div>
      </div>

      <div class="mt-4 space-y-4">
        <WdInput
          v-model="refundForm.amountYuan"
          type="number"
          label="退款金额"
          :min="0"
          :max="(detail?.pay_amount ?? 0) / 100"
          :hint="`单位：${symbol}（元）`"
        />
        <WdInput v-model="refundForm.reason" type="textarea" label="退款原因" :rows="2" :maxlength="200" />
      </div>

      <template #footer>
        <WdButton @click="refundVisible = false">取消</WdButton>
        <WdButton variant="danger" :loading="refunding" @click="submitRefund">确认退款</WdButton>
      </template>
    </WdModal>
  </div>
</template>
