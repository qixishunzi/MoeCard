<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  ChevronRight,
  Download,
  Eye,
  EyeOff,
  PackageSearch,
  Plus,
  Search,
  Trash2,
} from 'lucide-vue-next'
import { ApiError, adminApi } from '@/api'
import type { CodeImportResult, Product, ProductCode, ProductStock } from '@/api/types'
import { copyText, formatDateTime } from '@/utils/format'
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
const router = useRouter()

/**
 * 只有一个卡密页面，商品是它的一个筛选条件，记在 URL 上：
 *
 *   /admin/codes                 侧边栏进来 —— 全部商品
 *   /admin/codes?product_id=3    从商品列表的「卡密」进来 —— 只看这个商品
 *
 * 之所以不做成两个页面：卡密的日常管理是跨商品的（谁快没货了、这条卖给了谁），
 * 拆成两套视图就又回到"从商品里跳转做不到统一管理"的老问题上。
 * 筛选写在 URL 里，刷新、收藏、后退都能回到同一屏。
 */
const filterProductID = ref(Number(route.query.product_id) || 0)

/** 商品下拉选项（导入时必须指定商品，筛选也要用） */
const productOptions = ref<{ label: string; value: string }[]>([])
const autoProducts = ref<Product[]>([])
const inventory = ref<ProductStock[]>([])

const exporting = ref(false)

/** 导出的是解密后的明文卡密 —— 导出一份密文没有任何意义 */
async function exportCodes() {
  exporting.value = true
  try {
    const params = {
      status: query.status || undefined,
      keyword: query.keyword || undefined,
      order_no: query.order_no || undefined,
    }
    const day = new Date().toISOString().slice(0, 10)
    // 导出跟着当前筛选走 —— 屏幕上看到的是什么，导出的就是什么
    await adminApi.download(
      adminApi.exportAllCodesURL({ ...params, product_id: filterProductID.value || undefined }),
      `卡密-${scopeFileName.value}-${day}.csv`,
    )
    toast.success('导出完成')
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '导出失败')
  } finally {
    exporting.value = false
  }
}
const list = ref<ProductCode[]>([])
const stats = ref<Record<string, number>>({})
const total = ref(0)
const loading = ref(false)
const revealed = ref(false)

const query = reactive({ status: '', keyword: '', order_no: '', page: 1, page_size: 20 })

const importVisible = ref(false)
/** 导入目标商品。总览模式下必须手动选，单商品模式下就是当前商品 */
const importProductID = ref(0)
const importText = ref('')
const importing = ref(false)
const importResult = ref<CodeImportResult | null>(null)

const selected = ref<Set<number>>(new Set())

const statusMeta: Record<string, { text: string; tone: 'teal' | 'amber' | 'slate' }> = {
  unused: { text: '未使用', tone: 'teal' },
  locked: { text: '已锁定', tone: 'amber' },
  sold: { text: '已售出', tone: 'slate' },
}

const columns = computed<Column[]>(() => [
  { key: 'select', label: '', width: '44px', align: 'center' },
  { key: 'id', label: 'ID', width: '80px' },
  // 未按商品筛选时一屏能看到几十个商品的卡密，不标商品名根本认不出是哪个
  ...(filterProductID.value ? [] : [{ key: 'product', label: '商品', width: '180px' } as Column]),
  { key: 'content', label: '卡密内容' },
  { key: 'status', label: '状态', width: '100px', align: 'center' },
  { key: 'order_no', label: '关联订单', width: '210px', hideOnMobile: true },
  { key: 'time', label: '时间', width: '170px', hideOnMobile: true },
])

const selectableIds = computed(() =>
  list.value.filter((c) => c.status === 'unused').map((c) => c.id),
)
const allSelected = computed(
  () => selectableIds.value.length > 0 && selectableIds.value.every((id) => selected.value.has(id)),
)

/**
 * 拉商品列表填下拉框。
 *
 * 只保留自动发货商品：手动发货商品压根不用卡密库存，
 * 把它们放进"选择商品导入卡密"的下拉里只会让人选错。
 */
async function loadProducts() {
  try {
    // 后端把 page_size 封顶在 100，商品多于 100 个时必须翻页取完，
    // 否则下拉框里会缺商品 —— 而缺掉的那个恰恰可能就是要导卡密的那个。
    const all: Product[] = []
    for (let page = 1; page <= 10; page++) {
      const res = await adminApi.products({ page, page_size: 100, delivery_type: 'auto' })
      const rows = res.list ?? []
      all.push(...rows)
      if (rows.length < 100 || all.length >= res.total) break
    }
    autoProducts.value = all
    productOptions.value = all.map((p) => ({ label: p.name, value: String(p.id) }))
  } catch {
    productOptions.value = []
  }
}

async function loadInventory() {
  try {
    inventory.value = await adminApi.codeInventory()
  } catch {
    inventory.value = []
  }
}

async function loadStats() {
  try {
    stats.value = filterProductID.value
      ? await adminApi.codeStats(filterProductID.value)
      : await adminApi.allCodeStats()
  } catch {
    stats.value = {}
  }
}

async function load() {
  loading.value = true
  try {
    const params = {
      status: query.status || undefined,
      keyword: query.keyword || undefined,
      order_no: query.order_no || undefined,
      reveal: revealed.value || undefined,
      page: query.page,
      page_size: query.page_size,
    }
    const res = await adminApi.allCodes({
      ...params,
      product_id: filterProductID.value || undefined,
    })
    list.value = res.list ?? []
    total.value = res.total
    selected.value = new Set()
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

/**
 * 切换商品筛选。
 *
 * 同时做三件事：统计口径跟着变（否则头部数字和表格对不上）、
 * 写回 URL（刷新和后退都能回到同一屏）、回到第一页。
 */
function changeProduct() {
  query.page = 1
  router.replace({
    name: 'admin-codes',
    query: filterProductID.value ? { product_id: String(filterProductID.value) } : {},
  })
  Promise.all([load(), loadStats()])
}

function toggleSelect(id: number) {
  if (selected.value.has(id)) selected.value.delete(id)
  else selected.value.add(id)
  selected.value = new Set(selected.value)
}

function toggleSelectAll() {
  selected.value = allSelected.value ? new Set() : new Set(selectableIds.value)
}

/** 切换明文显示。查看明文会被记入管理员操作日志。 */
async function toggleReveal() {
  if (!revealed.value) {
    const ok = await confirmDialog({
      title: '查看卡密明文',
      message: '显示卡密明文会记入管理员操作日志。确定继续吗？',
      confirmText: '显示明文',
    })
    if (!ok) return
  }
  revealed.value = !revealed.value
  await load()
}

const importCount = computed(
  () =>
    importText.value
      .split(/\r?\n/)
      .map((s) => s.trim())
      .filter(Boolean).length,
)

function openImport() {
  // 已经按某个商品筛选时，默认就导进那个商品，省一次选择
  importProductID.value = filterProductID.value
  importResult.value = null
  importVisible.value = true
}

async function doImport() {
  const target = importProductID.value
  if (!target) {
    toast.error('请先选择要导入到哪个商品')
    return
  }
  if (!importCount.value) {
    toast.error('请输入要导入的卡密，一行一个')
    return
  }
  importing.value = true
  importResult.value = null
  try {
    const res = await adminApi.importAnyCodes(target, importText.value)
    importResult.value = res
    toast.success(`成功导入 ${res.imported} 条`)
    importText.value = ''
    await refresh()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '导入失败')
  } finally {
    importing.value = false
  }
}

/** 一次刷新全部受影响的数据 —— 导入/删除后这几处都会变 */
function refresh() {
  return Promise.all([load(), loadStats(), loadInventory()])
}

async function deleteSelected() {
  const ids = [...selected.value]
  if (!ids.length) {
    toast.warning('请选择未使用的卡密（已锁定/已售出的卡密不能删除）')
    return
  }
  const ok = await confirmDialog({
    title: '删除卡密',
    message: `确定删除选中的 ${ids.length} 条未使用卡密吗？`,
    confirmText: '删除',
    tone: 'danger',
  })
  if (!ok) return

  try {
    // 勾选的行可能跨多个商品，走不绑定商品的接口
    const res = await adminApi.deleteAnyCodes(ids)
    toast.success(`已删除 ${res.deleted} 条`)
    await refresh()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '删除失败')
  }
}

/**
 * 清空某个商品的全部未使用卡密。
 *
 * 刻意不提供"清空全站"：那是一次点击就能毁掉整个库存的操作，
 * 而且没有任何正当的日常用途。必须先筛到具体商品。
 */
async function deleteAllUnused() {
  if (!filterProductID.value) {
    toast.warning('请先在上方选择一个商品，再清空它的未使用卡密')
    return
  }
  const n = stats.value.unused ?? 0
  if (!n) {
    toast.info('没有未使用的卡密')
    return
  }
  const ok = await confirmDialog({
    title: '危险操作',
    message: `确定清空${scopeName.value}的全部 ${n} 条未使用卡密吗？此操作不可恢复。\n已锁定与已售出的卡密不会被删除。`,
    confirmText: '确定清空',
    tone: 'danger',
  })
  if (!ok) return

  try {
    const res = await adminApi.deleteCodes(filterProductID.value, { all_unused: true })
    toast.success(`已删除 ${res.deleted} 条`)
    await refresh()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '删除失败')
  }
}

async function copyCode(code: ProductCode) {
  if (!code.content) {
    toast.warning('请先点击「显示明文」')
    return
  }
  const ok = await copyText(code.content)
  ok ? toast.success('已复制') : toast.error('复制失败')
}

const statCards = computed(() => [
  { label: '可用库存', value: stats.value.unused ?? 0, tone: 'text-[#4a9d9a]' },
  { label: '已锁定', value: stats.value.locked ?? 0, tone: 'text-[#8f7243]' },
  { label: '已售出', value: stats.value.sold ?? 0, tone: 'text-gray-700' },
  { label: '总计', value: stats.value.total ?? 0, tone: 'text-gray-700' },
])

/** 当前数据范围的名字，用在标题和二次确认文案里 */
const scopeName = computed(() => {
  if (!filterProductID.value) return '全部商品'
  const hit = autoProducts.value.find((p) => p.id === filterProductID.value)
  if (hit) return `「${hit.name}」`
  // 商品已删除或已改成手动发货时它不在下拉里，但库存概览还留着它的名字。
  // 这种孤儿库存照样要能查看，不能因为认不出名字就显示成一串 ID。
  const orphan = inventory.value.find((r) => r.product_id === filterProductID.value)
  return orphan ? `「${orphan.product_name}」` : '所选商品'
})

/** 导出文件名里的范围标识，去掉文件名里不能用的字符 */
const scopeFileName = computed(() =>
  filterProductID.value ? scopeName.value.replace(/[「」\/:*?"<>|]/g, '') : '全部',
)

/**
 * 库存告急的商品：可用卡密 5 条以内。
 *
 * 这里用固定阈值而不是读商品各自的告警阈值 ——
 * 这只是个"该补货了"的一眼提示，真正的告警在「商家通知」里按阈值推送。
 */
const lowStock = computed(() => inventory.value.filter((r) => r.unused <= 5).slice(0, 6))

function clearProductFilter() {
  filterProductID.value = 0
  changeProduct()
}

function gotoProduct(id: number) {
  filterProductID.value = id
  changeProduct()
}

/**
 * URL 里的商品变了就跟着变。
 *
 * 同一条路由内部改 query 不会重新挂载组件，所以初始化时读一次 query 是不够的：
 * 手动改地址栏、或者从别处再次跳进来带了不同的 product_id，都要能同步过来。
 */
watch(
  () => route.query.product_id,
  (v) => {
    const id = Number(v) || 0
    if (id === filterProductID.value) return
    filterProductID.value = id
    query.page = 1
    Promise.all([load(), loadStats()])
  },
)

onMounted(async () => {
  await Promise.all([loadProducts(), loadStats(), load(), loadInventory()])
})
</script>

<template>
  <div class="space-y-5">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <p class="text-sm text-gray-500">
        当前统计范围：<span class="text-gray-700 font-medium">{{ scopeName }}</span>
        <button
          v-if="filterProductID"
          class="ml-2 text-xs text-[#4a9d9a] hover:underline"
          @click="clearProductFilter"
        >
          查看全部商品
        </button>
      </p>

      <div class="flex gap-2.5">
        <WdButton variant="primary" @click="openImport">
          <Plus class="w-4 h-4" />
          导入卡密
        </WdButton>
        <WdButton variant="danger" @click="deleteAllUnused">清空未使用</WdButton>
      </div>
    </div>

    <!-- 统计 -->
    <div class="grid grid-cols-2 lg:grid-cols-4 gap-5">
      <div
        v-for="s in statCards"
        :key="s.label"
        class="bg-white rounded-2xl shadow-xl shadow-black/[0.04] p-5 flex flex-col items-center gap-1"
      >
        <span class="text-2xl font-semibold tabular" :class="s.tone">{{ s.value }}</span>
        <span class="text-xs text-gray-400 text-center">{{ s.label }}</span>
      </div>
    </div>

    <!-- 库存概览：只在总览模式出现，回答"哪个商品快没货了" -->
    <WdCard
      v-if="inventory.length"
      title="库存概览"
      subtitle="按可用卡密从少到多排列，点一行即可只看它"
    >
      <template #actions>
        <WdBadge v-if="lowStock.length" tone="clay"> {{ lowStock.length }} 个商品库存告急 </WdBadge>
      </template>

      <div class="grid sm:grid-cols-2 lg:grid-cols-3 gap-3">
        <button
          v-for="row in inventory.slice(0, 9)"
          :key="row.product_id"
          class="group flex items-center justify-between gap-3 px-4 py-3 rounded-xl bg-[#faf8f5] hover:bg-[#4a9d9a]/10 transition-colors duration-200 text-left"
          :class="filterProductID === row.product_id && 'ring-2 ring-[#4a9d9a]/40'"
          @click="gotoProduct(row.product_id)"
        >
          <div class="min-w-0">
            <p class="text-sm text-gray-800 truncate">{{ row.product_name }}</p>
            <p class="text-xs text-gray-500 tabular">
              已锁定 {{ row.locked }} · 已售 {{ row.sold }}
            </p>
          </div>
          <div class="flex items-center gap-1 shrink-0">
            <span
              class="text-lg font-semibold tabular"
              :class="
                row.unused === 0
                  ? 'text-[#c17767]'
                  : row.unused <= 5
                    ? 'text-[#8f7243]'
                    : 'text-[#4a9d9a]'
              "
            >
              {{ row.unused }}
            </span>
            <ChevronRight
              class="w-4 h-4 text-gray-300 group-hover:text-[#4a9d9a] transition-colors duration-200"
            />
          </div>
        </button>
      </div>

      <p v-if="inventory.length > 9" class="mt-3 text-xs text-gray-500">
        仅显示可用卡密最少的 9 个商品，其余 {{ inventory.length - 9 }} 个可用下方筛选查看。
      </p>
    </WdCard>

    <WdCard flush>
      <div class="px-6 py-5">
        <div class="flex flex-wrap items-end gap-3 mb-5">
          <WdInput
            :model-value="filterProductID ? String(filterProductID) : ''"
            type="select"
            placeholder="全部商品"
            clearable
            class="w-52"
            :options="productOptions"
            @update:model-value="(v: string | number | null) => (filterProductID = Number(v) || 0)"
            @change="changeProduct"
          />
          <WdInput
            v-model="query.status"
            type="select"
            placeholder="全部状态"
            clearable
            class="w-32"
            :options="[
              { label: '未使用', value: 'unused' },
              { label: '已锁定', value: 'locked' },
              { label: '已售出', value: 'sold' },
            ]"
            @change="search"
          />
          <div class="relative w-48">
            <Search class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-300 z-10" />
            <input
              v-model="query.keyword"
              placeholder="搜索卡密内容"
              aria-label="搜索卡密内容"
              class="w-full pl-9 pr-3.5 py-2.5 bg-white border border-gray-200 rounded-xl text-sm text-gray-800 placeholder:text-gray-300 focus:outline-none focus:ring-2 focus:ring-[#4a9d9a]/30 focus:border-[#4a9d9a] transition-all duration-200"
              @keyup.enter="search"
            />
          </div>
          <WdInput
            v-model="query.order_no"
            placeholder="按订单号查询"
            class="w-52"
            @enter="search"
          />
          <WdButton variant="primary" @click="search">查询</WdButton>
          <WdButton :loading="exporting" @click="exportCodes">
            <Download class="w-4 h-4" />
            导出
          </WdButton>

          <div class="flex-1" />

          <WdButton :variant="revealed ? 'warning' : 'secondary'" @click="toggleReveal">
            <component :is="revealed ? EyeOff : Eye" class="w-4 h-4" />
            {{ revealed ? '隐藏明文' : '显示明文' }}
          </WdButton>
          <WdButton :disabled="!selected.size" @click="deleteSelected">
            <Trash2 class="w-4 h-4" />
            删除选中 ({{ selected.size }})
          </WdButton>
        </div>

        <WdTable
          :columns="columns"
          :rows="list"
          :loading="loading"
          empty-text="还没有卡密，点击右上角导入"
        >
          <template #product="{ row }">
            <button
              class="flex items-center gap-1 text-sm text-gray-700 hover:text-[#4a9d9a] transition-colors duration-200 min-w-0"
              @click="gotoProduct(row.product_id as number)"
            >
              <PackageSearch class="w-3.5 h-3.5 shrink-0 text-gray-300" />
              <span class="truncate">{{ row.product_name || `#${row.product_id}` }}</span>
            </button>
          </template>

          <template #select="{ row }">
            <input
              v-if="row.status === 'unused'"
              type="checkbox"
              class="accent-[#4a9d9a]"
              :checked="selected.has(row.id)"
              :aria-label="`选择卡密 ${row.id}`"
              @change="toggleSelect(row.id)"
            />
          </template>

          <template #id="{ row }">
            <span class="tabular text-gray-400">{{ row.id }}</span>
          </template>

          <template #content="{ row }">
            <div class="flex items-center gap-3">
              <span class="font-mono text-[13px] text-gray-700 break-all">
                {{ revealed && row.content ? row.content : row.masked_content }}
              </span>
              <button
                v-if="revealed"
                class="shrink-0 text-xs font-medium text-[#4a9d9a] hover:underline"
                @click="copyCode(row as ProductCode)"
              >
                复制
              </button>
            </div>
          </template>

          <template #status="{ row }">
            <WdBadge :tone="statusMeta[row.status]?.tone ?? 'gray'">
              {{ statusMeta[row.status]?.text ?? row.status }}
            </WdBadge>
          </template>

          <template #order_no="{ row }">
            <button
              v-if="row.order_no"
              class="font-mono text-xs text-[#4a9d9a] hover:underline"
              @click="router.push({ name: 'admin-orders', query: { keyword: row.order_no } })"
            >
              {{ row.order_no }}
            </button>
            <span v-else class="text-gray-300">—</span>
          </template>

          <template #time="{ row }">
            <div class="min-w-0">
              <p class="text-xs text-gray-600 tabular">
                {{ formatDateTime((row.sold_at || row.locked_at) as string | null) }}
              </p>
              <p class="text-[11px] text-gray-400">
                {{
                  row.sold_at
                    ? '售出'
                    : row.locked_at
                      ? '锁定'
                      : `导入于 ${formatDateTime(row.created_at as string)}`
                }}
              </p>
            </div>
          </template>

          <template #empty>
            <span>
              还没有卡密，
              <button class="text-[#4a9d9a] hover:underline" @click="openImport">点这里导入</button>
            </span>
          </template>
        </WdTable>

        <div v-if="selectableIds.length" class="pt-3">
          <label class="inline-flex items-center gap-2 text-xs text-gray-400 cursor-pointer">
            <input
              type="checkbox"
              class="accent-[#4a9d9a]"
              :checked="allSelected"
              @change="toggleSelectAll"
            />
            全选本页未使用卡密（{{ selectableIds.length }} 条）
          </label>
        </div>

        <WdPagination
          v-model:page="query.page"
          v-model:page-size="query.page_size"
          :total="total"
          @change="load"
        />
      </div>
    </WdCard>

    <!-- 导入 -->
    <WdModal v-model="importVisible" title="批量导入卡密" width="620px" :close-on-overlay="false">
      <!-- 总览模式下不指定商品就不知道该导到哪，必选 -->
      <WdInput
        :model-value="importProductID ? String(importProductID) : ''"
        type="select"
        label="导入到哪个商品"
        required
        placeholder="请选择商品"
        class="mb-4"
        :options="productOptions"
        hint="只列出自动发货商品 —— 手动发货商品不使用卡密库存"
        @update:model-value="(v: string | number | null) => (importProductID = Number(v) || 0)"
      />

      <div class="px-4 py-3.5 rounded-xl bg-[#faf8f5] text-xs text-gray-500 leading-relaxed">
        一行一个卡密。系统会自动：去除首尾空格、忽略空行、本批次内去重、
        与已有卡密去重（同一商品下相同卡密不会重复导入）。
      </div>

      <textarea
        v-model="importText"
        rows="12"
        placeholder="AAAA-BBBB-CCCC&#10;DDDD-EEEE-FFFF&#10;GGGG-HHHH-IIII"
        class="mt-4 w-full px-3.5 py-3 bg-white border border-gray-200 rounded-xl font-mono text-[13px] leading-relaxed text-gray-800 placeholder:text-gray-300 focus:outline-none focus:ring-2 focus:ring-[#4a9d9a]/30 focus:border-[#4a9d9a] transition-all duration-200 resize-y"
      />
      <p class="mt-2 text-xs text-gray-400">
        当前识别到
        <span class="font-medium text-gray-700 tabular">{{ importCount }}</span> 条（单次最多 20000
        条）
      </p>

      <div
        v-if="importResult"
        class="mt-4 px-4 py-3 rounded-xl text-xs leading-relaxed"
        :class="
          importResult.duplicate > 0
            ? 'bg-[#e8b86d]/10 text-[#b8873f]'
            : 'bg-[#4a9d9a]/10 text-[#4a9d9a]'
        "
      >
        提交 {{ importResult.total }} 条，成功导入
        <span class="font-medium">{{ importResult.imported }}</span> 条， 跳过重复
        <span class="font-medium">{{ importResult.duplicate }}</span> 条。
      </div>

      <template #footer>
        <WdButton @click="importVisible = false">关闭</WdButton>
        <WdButton
          variant="primary"
          :loading="importing"
          :disabled="!importCount || !importProductID"
          @click="doImport"
        >
          导入 {{ importCount || '' }}
        </WdButton>
      </template>
    </WdModal>
  </div>
</template>
