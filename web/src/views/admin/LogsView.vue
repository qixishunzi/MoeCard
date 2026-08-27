<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { RotateCw, Search } from 'lucide-vue-next'
import { ApiError, adminApi } from '@/api'
import type { EmailLog, NotifyLog, OperationLog, PaymentLog } from '@/api/types'
import { useShopStore } from '@/stores/shop'
import { copyText, formatDateTime, formatMoney } from '@/utils/format'
import {
  WdBadge,
  WdButton,
  WdCard,
  WdInput,
  WdModal,
  WdPagination,
  WdTable,
  WdTabs,
  toast,
  type Column,
} from '@/ui'

const shop = useShopStore()
const symbol = computed(() => shop.symbol())

const tab = ref('operations')
const tabs = [
  { value: 'operations', label: '操作日志' },
  { value: 'payments', label: '支付日志' },
  { value: 'emails', label: '邮件日志' },
  { value: 'notify', label: '通知日志' },
]

const loading = ref(false)
const total = ref(0)
const page = reactive({ page: 1, page_size: 20 })

const opLogs = ref<OperationLog[]>([])
const payLogs = ref<PaymentLog[]>([])
const mailLogs = ref<EmailLog[]>([])
const notifyLogs = ref<NotifyLog[]>([])

const filters = reactive({
  keyword: '',
  action: '',
  order_no: '',
  provider: '',
  event: '',
  email: '',
  status: '',
})

const detailVisible = ref(false)
const detail = ref<PaymentLog | null>(null)

/** 操作类型 → 中文。与后端 model.Action* 常量一一对应。 */
const ACTION_LABELS: Record<string, string> = {
  login: '登录',
  logout: '退出登录',
  change_password: '修改密码',
  create_category: '创建分类',
  update_category: '修改分类',
  delete_category: '删除分类',
  create_product: '创建商品',
  update_product: '修改商品',
  delete_product: '删除商品',
  import_codes: '导入卡密',
  delete_codes: '删除卡密',
  deliver_order: '手动发货',
  remark_order: '订单备注',
  refund_order: '订单退款',
  resend_mail: '重发邮件',
  create_coupon: '创建优惠券',
  update_coupon: '修改优惠券',
  delete_coupon: '删除优惠券',
  create_payment_channel: '创建支付渠道',
  update_payment_channel: '修改支付渠道',
  delete_payment_channel: '删除支付渠道',
  update_settings: '修改设置',
  test_mail: '测试邮件',
  create_admin: '创建管理员',
  update_admin: '修改管理员',
  delete_admin: '删除管理员',
  upload_file: '上传文件',
  setup_system: '系统初始化',
}

/** 支付事件 → 中文 + 语义色。危险事件必须一眼能看出来。 */
const PAY_EVENTS: Record<
  string,
  { label: string; tone: 'teal' | 'amber' | 'clay' | 'slate' | 'gray' }
> = {
  create: { label: '发起支付', tone: 'slate' },
  create_failed: { label: '发起失败', tone: 'clay' },
  notify: { label: '收到回调', tone: 'slate' },
  notify_invalid: { label: '验签失败', tone: 'clay' },
  paid: { label: '支付成功', tone: 'teal' },
  duplicate: { label: '重复回调', tone: 'gray' },
  amount_mismatch: { label: '金额不符', tone: 'clay' },
  query: { label: '主动查询', tone: 'gray' },
  refund: { label: '退款', tone: 'amber' },
  refund_failed: { label: '退款失败', tone: 'clay' },
  manual_refund: { label: '人工退款', tone: 'amber' },
}

const MAIL_TEMPLATES: Record<string, string> = {
  paid: '支付成功',
  deliver: '自动发货',
  manual: '手动发货',
  test: '测试邮件',
}

const opColumns: Column[] = [
  { key: 'created_at', label: '时间', width: '160px' },
  { key: 'admin', label: '操作人', width: '150px' },
  { key: 'action', label: '操作', width: '130px' },
  { key: 'detail', label: '详情' },
  { key: 'ip', label: 'IP', width: '130px', hideOnMobile: true },
]

const payColumns: Column[] = [
  { key: 'created_at', label: '时间', width: '160px' },
  { key: 'order_no', label: '订单号', width: '190px' },
  { key: 'event', label: '事件', width: '110px', align: 'center' },
  { key: 'provider', label: '渠道', width: '110px' },
  { key: 'amount', label: '金额', width: '110px', align: 'right' },
  { key: 'trade_no', label: '交易号', hideOnMobile: true },
  { key: 'actions', label: '', width: '70px', align: 'center' },
]

const NOTIFY_EVENTS: Record<string, string> = {
  order_paid: '订单支付成功',
  manual_delivery: '待人工发货',
  needs_attention: '订单需要处理',
  low_stock: '库存告急',
  refund: '订单退款',
  test: '测试通知',
}

const NOTIFY_CHANNELS: Record<string, string> = {
  email: '邮件',
  telegram: 'Telegram',
  bark: 'Bark',
  wecom: '企业微信',
  webhook: '自定义 Webhook',
}

const notifyColumns: Column[] = [
  { key: 'created_at', label: '时间', width: '160px' },
  { key: 'channel', label: '渠道', width: '130px' },
  { key: 'event', label: '事件', width: '120px' },
  { key: 'title', label: '内容' },
  { key: 'status', label: '结果', width: '150px', align: 'center' },
]

const mailColumns: Column[] = [
  { key: 'created_at', label: '时间', width: '160px' },
  { key: 'to_email', label: '收件人', width: '200px' },
  { key: 'subject', label: '主题' },
  { key: 'template', label: '类型', width: '100px', align: 'center' },
  { key: 'status', label: '结果', width: '150px', align: 'center' },
]

async function load() {
  loading.value = true
  try {
    const base = { page: page.page, page_size: page.page_size }
    if (tab.value === 'operations') {
      const res = await adminApi.operationLogs({
        ...base,
        keyword: filters.keyword || undefined,
        action: filters.action || undefined,
      })
      opLogs.value = res.list ?? []
      total.value = res.total
    } else if (tab.value === 'payments') {
      const res = await adminApi.paymentLogs({
        ...base,
        order_no: filters.order_no || undefined,
        provider: filters.provider || undefined,
        event: filters.event || undefined,
      })
      payLogs.value = res.list ?? []
      total.value = res.total
    } else if (tab.value === 'notify') {
      const res = await adminApi.notifyLogs({
        ...base,
        channel: filters.provider || undefined,
        event: filters.event || undefined,
        status: filters.status || undefined,
      })
      notifyLogs.value = res.list ?? []
      total.value = res.total
    } else {
      const res = await adminApi.emailLogs({
        ...base,
        order_no: filters.order_no || undefined,
        email: filters.email || undefined,
        status: filters.status || undefined,
      })
      mailLogs.value = res.list ?? []
      total.value = res.total
    }
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
}

function search() {
  page.page = 1
  load()
}

/** 切换分页时清掉上一个 tab 的筛选条件，否则会带着不适用的参数查询 */
watch(tab, () => {
  Object.assign(filters, {
    keyword: '',
    action: '',
    order_no: '',
    provider: '',
    event: '',
    email: '',
    status: '',
  })
  page.page = 1
  total.value = 0
  load()
})

function openDetail(row: PaymentLog) {
  detail.value = row
  detailVisible.value = true
}

/** 支付报文多半是 JSON，格式化后才能读；不是 JSON 就原样显示。 */
function pretty(raw: string): string {
  if (!raw) return '（空）'
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

async function copyRaw(raw: string) {
  const ok = await copyText(raw)
  ok ? toast.success('已复制') : toast.error('复制失败')
}

onMounted(load)
</script>

<template>
  <div class="space-y-5">
    <div class="flex flex-col lg:flex-row lg:items-center justify-between gap-3">
      <WdTabs v-model="tab" :tabs="tabs" />

      <div class="flex flex-wrap items-center gap-2">
        <!-- 操作日志 -->
        <template v-if="tab === 'operations'">
          <div class="w-44">
            <WdInput
              v-model="filters.action"
              type="select"
              placeholder="全部操作"
              clearable
              :options="
                Object.entries(ACTION_LABELS).map(([v, l]) => ({
                  label: l,
                  value: v,
                }))
              "
              @change="search"
            />
          </div>
          <div class="w-52">
            <WdInput
              v-model="filters.keyword"
              type="search"
              placeholder="搜索操作人或详情"
              @enter="search"
            />
          </div>
        </template>

        <!-- 支付日志 -->
        <template v-else-if="tab === 'payments'">
          <div class="w-40">
            <WdInput
              v-model="filters.event"
              type="select"
              placeholder="全部事件"
              clearable
              :options="
                Object.entries(PAY_EVENTS).map(([v, e]) => ({
                  label: e.label,
                  value: v,
                }))
              "
              @change="search"
            />
          </div>
          <div class="w-36">
            <WdInput v-model="filters.provider" type="search" placeholder="渠道" @enter="search" />
          </div>
          <div class="w-48">
            <WdInput
              v-model="filters.order_no"
              type="search"
              placeholder="订单号"
              @enter="search"
            />
          </div>
        </template>

        <!-- 通知日志 -->
        <template v-else-if="tab === 'notify'">
          <div class="w-36">
            <WdInput
              v-model="filters.event"
              type="select"
              placeholder="全部事件"
              clearable
              :options="
                Object.entries(NOTIFY_EVENTS).map(([v, l]) => ({
                  label: l,
                  value: v,
                }))
              "
              @change="search"
            />
          </div>
          <div class="w-36">
            <WdInput
              v-model="filters.provider"
              type="select"
              placeholder="全部渠道"
              clearable
              :options="
                Object.entries(NOTIFY_CHANNELS).map(([v, l]) => ({
                  label: l,
                  value: v,
                }))
              "
              @change="search"
            />
          </div>
          <div class="w-28">
            <WdInput
              v-model="filters.status"
              type="select"
              placeholder="全部结果"
              clearable
              :options="[
                { label: '成功', value: 'success' },
                { label: '失败', value: 'failed' },
              ]"
              @change="search"
            />
          </div>
        </template>

        <!-- 邮件日志 -->
        <template v-else>
          <div class="w-32">
            <WdInput
              v-model="filters.status"
              type="select"
              placeholder="全部结果"
              clearable
              :options="[
                { label: '成功', value: 'success' },
                { label: '失败', value: 'failed' },
              ]"
              @change="search"
            />
          </div>
          <div class="w-48">
            <WdInput v-model="filters.email" type="search" placeholder="收件邮箱" @enter="search" />
          </div>
          <div class="w-44">
            <WdInput
              v-model="filters.order_no"
              type="search"
              placeholder="订单号"
              @enter="search"
            />
          </div>
        </template>

        <WdButton @click="search">
          <Search class="w-4 h-4" />
          查询
        </WdButton>
        <WdButton variant="ghost" :loading="loading" @click="load">
          <RotateCw class="w-4 h-4" />
        </WdButton>
      </div>
    </div>

    <WdCard flush>
      <div class="px-6 py-5">
        <!--
          三种日志的字段几乎不重叠，共用一个表格会让每个单元格的类型退化成
          unknown。分成三个各自带类型的表格，模板里的 row 才是真正被检查的。
        -->

        <Transition name="wd-tab" mode="out-in">
          <!-- 操作日志 -->
          <WdTable
            key="operations"
            v-if="tab === 'operations'"
            :columns="opColumns"
            :rows="opLogs"
            :loading="loading"
          >
            <template #created_at="{ row }">
              <span class="text-sm text-gray-500 tabular">{{
                formatDateTime(row.created_at)
              }}</span>
            </template>

            <template #admin="{ row }">
              <div class="min-w-0">
                <p class="text-sm text-gray-700 truncate">
                  {{ row.admin_username || '系统' }}
                </p>
                <p v-if="row.target_type" class="text-xs text-gray-400 truncate">
                  {{ row.target_type }}{{ row.target_id ? ` #${row.target_id}` : '' }}
                </p>
              </div>
            </template>

            <template #action="{ row }">
              <WdBadge :tone="row.action.startsWith('delete') ? 'clay' : 'slate'">
                {{ ACTION_LABELS[row.action] ?? row.action }}
              </WdBadge>
            </template>

            <template #detail="{ row }">
              <span class="text-sm text-gray-600 break-all">{{ row.detail || '—' }}</span>
            </template>

            <template #ip="{ row }">
              <span class="font-mono text-xs text-gray-400">{{ row.ip || '—' }}</span>
            </template>

            <template #empty>
              <span>暂无操作记录</span>
            </template>
          </WdTable>

          <!-- 支付日志 -->
          <WdTable
            v-else-if="tab === 'payments'"
            key="payments"
            :columns="payColumns"
            :rows="payLogs"
            :loading="loading"
          >
            <template #created_at="{ row }">
              <span class="text-sm text-gray-500 tabular">{{
                formatDateTime(row.created_at)
              }}</span>
            </template>

            <template #order_no="{ row }">
              <RouterLink
                v-if="row.order_no"
                :to="{
                  name: 'admin-orders',
                  query: { order_no: row.order_no },
                }"
                class="font-mono text-xs text-[#4a9d9a] hover:underline"
              >
                {{ row.order_no }}
              </RouterLink>
              <span v-else class="text-gray-300">—</span>
            </template>

            <template #event="{ row }">
              <WdBadge :tone="PAY_EVENTS[row.event]?.tone ?? 'gray'">
                {{ PAY_EVENTS[row.event]?.label ?? row.event }}
              </WdBadge>
            </template>

            <template #provider="{ row }">
              <span class="text-sm text-gray-600">{{ row.provider }}</span>
            </template>

            <template #amount="{ row }">
              <span class="text-sm text-gray-700 tabular">
                {{ row.amount ? formatMoney(row.amount, symbol) : '—' }}
              </span>
            </template>

            <template #trade_no="{ row }">
              <span class="font-mono text-xs text-gray-400 break-all">{{
                row.trade_no || '—'
              }}</span>
            </template>

            <template #actions="{ row }">
              <button
                class="text-xs font-medium text-[#4a9d9a] hover:underline"
                @click="openDetail(row)"
              >
                报文
              </button>
            </template>

            <template #empty>
              <span>暂无支付记录</span>
            </template>
          </WdTable>

          <!-- 通知日志 -->
          <WdTable
            v-else-if="tab === 'notify'"
            key="notify"
            :columns="notifyColumns"
            :rows="notifyLogs"
            :loading="loading"
          >
            <template #created_at="{ row }">
              <span class="text-sm text-gray-500 tabular">{{
                formatDateTime(row.created_at)
              }}</span>
            </template>
            <template #channel="{ row }">
              <span class="text-sm text-gray-600">
                {{ NOTIFY_CHANNELS[row.channel] ?? row.channel }}
              </span>
            </template>
            <template #event="{ row }">
              <WdBadge :tone="row.event === 'needs_attention' ? 'clay' : 'slate'">
                {{ NOTIFY_EVENTS[row.event] ?? row.event }}
              </WdBadge>
            </template>
            <template #title="{ row }">
              <div class="min-w-0">
                <p class="text-sm text-gray-700 truncate">{{ row.title }}</p>
                <p class="text-xs text-gray-500 truncate">{{ row.content }}</p>
              </div>
            </template>
            <template #status="{ row }">
              <div class="flex flex-col items-center gap-1">
                <WdBadge :tone="row.status === 'success' ? 'teal' : 'clay'" dot>
                  {{ row.status === 'success' ? '成功' : '失败' }}
                </WdBadge>
                <span
                  v-if="row.error"
                  class="text-[11px] text-[#c17767] truncate max-w-[140px]"
                  :title="row.error"
                >
                  {{ row.error }}
                </span>
              </div>
            </template>
            <template #empty>
              <span>暂无通知记录</span>
            </template>
          </WdTable>

          <!-- 邮件日志 -->
          <WdTable key="mail" v-else :columns="mailColumns" :rows="mailLogs" :loading="loading">
            <template #created_at="{ row }">
              <span class="text-sm text-gray-500 tabular">{{
                formatDateTime(row.created_at)
              }}</span>
            </template>

            <template #to_email="{ row }">
              <div class="min-w-0">
                <p class="text-sm text-gray-700 truncate">{{ row.to_email }}</p>
                <p v-if="row.order_no" class="font-mono text-[11px] text-gray-400 truncate">
                  {{ row.order_no }}
                </p>
              </div>
            </template>

            <template #subject="{ row }">
              <span class="text-sm text-gray-600 break-all">{{ row.subject }}</span>
            </template>

            <template #template="{ row }">
              <span class="text-xs text-gray-500">
                {{ MAIL_TEMPLATES[row.template] ?? row.template }}
              </span>
            </template>

            <template #status="{ row }">
              <div class="flex flex-col items-center gap-1">
                <WdBadge :tone="row.status === 'success' ? 'teal' : 'clay'" dot>
                  {{ row.status === 'success' ? '成功' : '失败' }}
                </WdBadge>
                <span
                  v-if="row.error"
                  class="text-[11px] text-[#c17767] truncate max-w-[140px]"
                  :title="row.error"
                >
                  {{ row.error }}
                </span>
              </div>
            </template>

            <template #empty>
              <span>暂无邮件记录</span>
            </template>
          </WdTable>
        </Transition>

        <WdPagination
          v-model:page="page.page"
          v-model:page-size="page.page_size"
          :total="total"
          @change="load"
        />
      </div>
    </WdCard>

    <p
      class="px-5 py-4 rounded-2xl bg-white shadow-xl shadow-black/[0.04] text-xs text-gray-500 leading-relaxed"
    >
      支付日志会保留每一次回调的原始报文，出现纠纷或掉单时这是唯一的证据链。 成功事件（<span
        class="text-[#4a9d9a]"
        >支付成功</span
      >、<span class="text-[#b8873f]">退款</span>）永久保留；
      发起支付与主动查询这类高频记录会定期清理，避免日志表无限膨胀。
    </p>

    <!-- 支付报文 -->
    <WdModal v-model="detailVisible" title="支付报文" width="720px">
      <div v-if="detail" class="space-y-5">
        <dl class="grid sm:grid-cols-2 gap-x-6 gap-y-3 text-sm">
          <div class="flex gap-3">
            <dt class="w-16 shrink-0 text-gray-400">订单号</dt>
            <dd class="font-mono text-xs text-gray-700 break-all">
              {{ detail.order_no || '—' }}
            </dd>
          </div>
          <div class="flex gap-3">
            <dt class="w-16 shrink-0 text-gray-400">事件</dt>
            <dd>
              <WdBadge :tone="PAY_EVENTS[detail.event]?.tone ?? 'gray'">
                {{ PAY_EVENTS[detail.event]?.label ?? detail.event }}
              </WdBadge>
            </dd>
          </div>
          <div class="flex gap-3">
            <dt class="w-16 shrink-0 text-gray-400">渠道</dt>
            <dd class="text-gray-700">{{ detail.provider }}</dd>
          </div>
          <div class="flex gap-3">
            <dt class="w-16 shrink-0 text-gray-400">金额</dt>
            <dd class="text-gray-700 tabular">
              {{ detail.amount ? formatMoney(detail.amount, symbol) : '—' }}
            </dd>
          </div>
          <div class="flex gap-3">
            <dt class="w-16 shrink-0 text-gray-400">交易号</dt>
            <dd class="font-mono text-xs text-gray-700 break-all">
              {{ detail.trade_no || '—' }}
            </dd>
          </div>
          <div class="flex gap-3">
            <dt class="w-16 shrink-0 text-gray-400">来源 IP</dt>
            <dd class="font-mono text-xs text-gray-700">
              {{ detail.client_ip || '—' }}
            </dd>
          </div>
          <div class="flex gap-3">
            <dt class="w-16 shrink-0 text-gray-400">时间</dt>
            <dd class="text-gray-700 tabular">
              {{ formatDateTime(detail.created_at) }}
            </dd>
          </div>
          <div v-if="detail.status" class="flex gap-3">
            <dt class="w-16 shrink-0 text-gray-400">状态</dt>
            <dd class="text-gray-700">{{ detail.status }}</dd>
          </div>
        </dl>

        <div>
          <div class="flex items-center justify-between mb-2">
            <p class="text-xs font-medium text-gray-500">请求报文</p>
            <button
              class="text-xs font-medium text-[#4a9d9a] hover:underline"
              @click="copyRaw(detail!.request_data)"
            >
              复制
            </button>
          </div>
          <pre
            class="max-h-64 overflow-auto px-4 py-3 rounded-xl bg-[#faf8f5] font-mono text-[11px] leading-relaxed text-gray-600 whitespace-pre-wrap break-all"
            >{{ pretty(detail.request_data) }}</pre>
        </div>

        <div>
          <div class="flex items-center justify-between mb-2">
            <p class="text-xs font-medium text-gray-500">响应 / 处理结果</p>
            <button
              class="text-xs font-medium text-[#4a9d9a] hover:underline"
              @click="copyRaw(detail!.response_data)"
            >
              复制
            </button>
          </div>
          <pre
            class="max-h-64 overflow-auto px-4 py-3 rounded-xl bg-[#faf8f5] font-mono text-[11px] leading-relaxed text-gray-600 whitespace-pre-wrap break-all"
            >{{ pretty(detail.response_data) }}</pre>
        </div>
      </div>
    </WdModal>
  </div>
</template>
