<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { Plus, RefreshCw, Search } from 'lucide-vue-next'
import { ApiError, adminApi } from '@/api'
import type { Coupon, CouponUsage, Product } from '@/api/types'
import { discountToPercent, formatAmount, formatPercentOff, parseAmount } from '@/utils/format'
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

const shop = useShopStore()
const symbol = computed(() => shop.symbol())

const list = ref<Coupon[]>([])
const total = ref(0)
const loading = ref(false)

const query = reactive({ keyword: '', type: '', status: '', page: 1, page_size: 20 })

const dialogVisible = ref(false)
const editing = ref<Coupon | null>(null)
const submitting = ref(false)

/** 表单用「元」和「折」这类人类友好的单位，提交前转成后端要的整数（分 / 万分比） */
const form = reactive({
  code: '',
  name: '',
  type: 'fixed' as 'fixed' | 'percent',
  fixedYuan: 10 as number | null,
  discount: 9 as number | null,
  minYuan: 0 as number | null,
  maxDiscountYuan: 0 as number | null,
  scope: 'all' as 'all' | 'products',
  product_ids: [] as number[],
  usage_limit: 0 as number | null,
  per_user_limit: 0 as number | null,
  start_at: '',
  expire_at: '',
  status: 'active' as 'active' | 'disabled',
})

const products = ref<Product[]>([])
const productKeyword = ref('')

const usageVisible = ref(false)
const usages = ref<CouponUsage[]>([])
const usageTotal = ref(0)
const usageCoupon = ref<Coupon | null>(null)

const columns: Column[] = [
  { key: 'code', label: '券码', width: '150px' },
  { key: 'value', label: '优惠', width: '120px' },
  { key: 'condition', label: '使用条件' },
  { key: 'scope', label: '适用范围', width: '130px', hideOnMobile: true },
  { key: 'used', label: '使用情况', width: '110px', align: 'center' },
  { key: 'period', label: '有效期', width: '175px', hideOnMobile: true },
  { key: 'status', label: '状态', width: '80px', align: 'center' },
  { key: 'actions', label: '操作', width: '120px', align: 'center' },
]

async function load() {
  loading.value = true
  try {
    const res = await adminApi.coupons({
      keyword: query.keyword || undefined,
      type: query.type || undefined,
      status: query.status || undefined,
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

async function searchProducts() {
  try {
    const res = await adminApi.products({
      keyword: productKeyword.value || undefined,
      page_size: 50,
    })
    products.value = res.list ?? []
  } catch {
    products.value = []
  }
}

function openCreate() {
  editing.value = null
  Object.assign(form, {
    code: '',
    name: '',
    type: 'fixed',
    fixedYuan: 10,
    discount: 9,
    minYuan: 0,
    maxDiscountYuan: 0,
    scope: 'all',
    product_ids: [],
    usage_limit: 0,
    per_user_limit: 0,
    start_at: '',
    expire_at: '',
    status: 'active',
  })
  dialogVisible.value = true
  searchProducts()
}

async function openEdit(row: Coupon) {
  try {
    const c = await adminApi.coupon(row.id)
    editing.value = c
    Object.assign(form, {
      code: c.code,
      name: c.name,
      type: c.type,
      fixedYuan: c.type === 'fixed' ? c.value / 100 : 10,
      discount: c.type === 'percent' ? c.value / 1000 : 9,
      minYuan: c.min_amount / 100,
      maxDiscountYuan: c.max_discount / 100,
      scope: c.scope,
      product_ids: c.product_ids ?? [],
      usage_limit: c.usage_limit,
      per_user_limit: c.per_user_limit,
      start_at: (c.start_at ?? '').slice(0, 10),
      expire_at: (c.expire_at ?? '').slice(0, 10),
      status: c.status,
    })
    dialogVisible.value = true
    // 把已关联的商品先放进候选列表，避免多选框显示成空白
    if (c.products?.length) products.value = c.products
    else searchProducts()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '加载失败')
  }
}

function toPayload() {
  return {
    code: form.code.trim().toUpperCase(),
    name: form.name.trim(),
    type: form.type,
    // fixed → 分；percent → 万分比（9 折 = 9000）
    value:
      form.type === 'fixed'
        ? parseAmount(form.fixedYuan ?? 0)
        : discountToPercent(form.discount ?? 9),
    min_amount: parseAmount(form.minYuan ?? 0),
    max_discount: parseAmount(form.maxDiscountYuan ?? 0),
    scope: form.scope,
    product_ids: form.scope === 'products' ? form.product_ids : [],
    usage_limit: form.usage_limit ?? 0,
    per_user_limit: form.per_user_limit ?? 0,
    // 日期补上时间部分，交给后端按 UTC 解析
    start_at: form.start_at ? `${form.start_at}T00:00:00Z` : '',
    expire_at: form.expire_at ? `${form.expire_at}T23:59:59Z` : '',
    status: form.status,
  }
}

async function submit() {
  if (form.type === 'fixed' && parseAmount(form.fixedYuan ?? 0) <= 0) {
    toast.error('优惠金额必须大于 0')
    return
  }
  if (form.type === 'percent' && ((form.discount ?? 0) <= 0 || (form.discount ?? 0) >= 10)) {
    toast.error('折扣需在 0 ~ 10 之间（9 折填 9）')
    return
  }
  if (form.scope === 'products' && !form.product_ids.length) {
    toast.error('选择「指定商品」时必须至少关联一个商品')
    return
  }

  submitting.value = true
  try {
    if (editing.value) {
      await adminApi.updateCoupon(editing.value.id, toPayload())
      toast.success('优惠券已更新')
    } else {
      await adminApi.createCoupon(toPayload())
      toast.success('优惠券已创建')
    }
    dialogVisible.value = false
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    submitting.value = false
  }
}

async function remove(row: Coupon) {
  const ok = await confirmDialog({
    title: '删除优惠券',
    message: `确定删除优惠券「${row.code}」吗？\n历史核销记录会保留，不影响已完成的订单。`,
    confirmText: '删除',
    tone: 'danger',
  })
  if (!ok) return
  try {
    await adminApi.deleteCoupon(row.id)
    toast.success('优惠券已删除')
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '删除失败')
  }
}

async function openUsages(row: Coupon) {
  usageCoupon.value = row
  usageVisible.value = true
  try {
    const res = await adminApi.couponUsages(row.id, { page: 1, page_size: 50 })
    usages.value = res.list ?? []
    usageTotal.value = res.total
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '加载失败')
  }
}

function valueText(c: Coupon) {
  return c.type === 'fixed' ? `减 ${symbol.value}${formatAmount(c.value)}` : formatPercentOff(c.value)
}

function randomCode() {
  const chars = '23456789ABCDEFGHJKMNPQRSTUVWXYZ'
  let s = ''
  for (let i = 0; i < 10; i++) s += chars[Math.floor(Math.random() * chars.length)]
  form.code = s
}

function toggleProduct(id: number) {
  const i = form.product_ids.indexOf(id)
  if (i >= 0) form.product_ids.splice(i, 1)
  else form.product_ids.push(id)
}

watch(
  () => form.scope,
  (v) => {
    if (v === 'products' && !products.value.length) searchProducts()
  },
)

/** 本页里还能用的券：状态启用、没过期、也没用完 */
const usableCount = computed(
  () =>
    list.value.filter((c) => {
      if (c.status !== 'active') return false
      if (c.expire_at && new Date(c.expire_at).getTime() < Date.now()) return false
      return !c.usage_limit || c.used_count < c.usage_limit
    }).length,
)

onMounted(load)
</script>

<template>
  <div class="space-y-5">
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
      <p class="text-xs text-gray-500">
        共 <span class="tabular text-gray-700">{{ total }}</span> 张券，本页
        <span class="tabular text-gray-700">{{ usableCount }}</span> 张可用
      </p>
      <WdButton variant="primary" @click="openCreate">
        <Plus class="w-4 h-4" />
        新建优惠券
      </WdButton>
    </div>

    <p class="px-5 py-3.5 rounded-2xl bg-white shadow-xl shadow-black/[0.04] text-xs text-gray-500 leading-relaxed">
      优惠券在<span class="font-medium text-gray-700">支付成功后</span>才会核销扣减次数。
      用户创建未支付订单不会消耗额度，因此不用担心被刷。
    </p>

    <WdCard flush>
      <div class="px-6 py-5">
        <div class="flex flex-wrap items-end gap-3 mb-5">
          <div class="relative w-52">
            <Search class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-300 z-10" />
            <input
              v-model="query.keyword"
              placeholder="搜索券码 / 名称"
              aria-label="搜索券码或名称"
              class="w-full pl-9 pr-3.5 py-2.5 bg-white border border-gray-200 rounded-xl text-sm text-gray-800 placeholder:text-gray-300 focus:outline-none focus:ring-2 focus:ring-[#4a9d9a]/30 focus:border-[#4a9d9a] transition-all duration-200"
              @keyup.enter="search"
            />
          </div>
          <WdInput
            v-model="query.type"
            type="select"
            placeholder="全部类型"
            clearable
            class="w-36"
            :options="[
              { label: '固定金额', value: 'fixed' },
              { label: '百分比折扣', value: 'percent' },
            ]"
            @change="search"
          />
          <WdInput
            v-model="query.status"
            type="select"
            placeholder="全部状态"
            clearable
            class="w-32"
            :options="[
              { label: '启用', value: 'active' },
              { label: '停用', value: 'disabled' },
            ]"
            @change="search"
          />
          <WdButton variant="primary" @click="search">查询</WdButton>
        </div>

        <WdTable :columns="columns" :rows="list" :loading="loading" empty-text="还没有优惠券">
          <template #code="{ row }">
            <p class="font-mono text-sm font-medium text-gray-800">{{ row.code }}</p>
            <p class="text-xs text-gray-400 line-clamp-1">{{ row.name }}</p>
          </template>

          <template #value="{ row }">
            <WdBadge :tone="row.type === 'fixed' ? 'amber' : 'teal'">
              {{ valueText(row as Coupon) }}
            </WdBadge>
          </template>

          <template #condition="{ row }">
            <div class="text-xs text-gray-500 space-y-0.5">
              <p v-if="row.min_amount > 0">满 {{ symbol }}{{ formatAmount(row.min_amount) }} 可用</p>
              <p v-if="row.type === 'percent' && row.max_discount > 0">
                最多减 {{ symbol }}{{ formatAmount(row.max_discount) }}
              </p>
              <p v-if="row.per_user_limit > 0">每人限用 {{ row.per_user_limit }} 次</p>
              <p v-if="!row.min_amount && !row.max_discount && !row.per_user_limit" class="text-gray-300">
                无限制
              </p>
            </div>
          </template>

          <template #scope="{ row }">
            <WdBadge :tone="row.scope === 'all' ? 'slate' : 'teal'">
              {{ row.scope === 'all' ? '全部商品' : `指定 ${row.product_ids?.length ?? 0} 个` }}
            </WdBadge>
          </template>

          <template #used="{ row }">
            <button
              class="text-sm font-medium text-[#4a9d9a] hover:underline tabular"
              @click="openUsages(row as Coupon)"
            >
              {{ row.used_count }} / {{ row.usage_limit || '∞' }}
            </button>
          </template>

          <template #period="{ row }">
            <div v-if="row.start_at || row.expire_at" class="text-xs text-gray-500 tabular">
              {{ (row.start_at || '不限').slice(0, 10) }}
              <br />~ {{ (row.expire_at || '不限').slice(0, 10) }}
            </div>
            <span v-else class="text-xs text-gray-300">永久有效</span>
          </template>

          <template #status="{ row }">
            <WdBadge :tone="row.status === 'active' ? 'teal' : 'gray'">
              {{ row.status === 'active' ? '启用' : '停用' }}
            </WdBadge>
          </template>

          <template #actions="{ row }">
            <div class="flex items-center justify-center gap-3">
              <button
                class="text-xs font-medium text-[#4a9d9a] hover:underline"
                @click="openEdit(row as Coupon)"
              >
                编辑
              </button>
              <button
                class="text-xs font-medium text-[#c17767] hover:underline"
                @click="remove(row as Coupon)"
              >
                删除
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

    <!-- 编辑 -->
    <WdModal
      v-model="dialogVisible"
      :title="editing ? '编辑优惠券' : '新建优惠券'"
      width="620px"
      :close-on-overlay="false"
    >
      <div class="space-y-4">
        <div>
          <label for="coupon-code" class="block mb-1.5 text-xs font-medium text-gray-500">
            券码
          </label>
          <div class="flex gap-2.5">
            <input
              id="coupon-code"
              v-model="form.code"
              placeholder="留空自动生成"
              maxlength="32"
              class="flex-1 px-3.5 py-2.5 bg-white border border-gray-200 rounded-xl font-mono text-sm text-gray-800 placeholder:text-gray-300 focus:outline-none focus:ring-2 focus:ring-[#4a9d9a]/30 focus:border-[#4a9d9a] transition-all duration-200"
            />
            <WdButton @click="randomCode">
              <RefreshCw class="w-4 h-4" />
              随机
            </WdButton>
          </div>
          <p class="mt-1.5 text-xs text-gray-400">用户在结算页输入此码使用优惠，不区分大小写</p>
        </div>

        <WdInput v-model="form.name" label="名称" placeholder="如：新人专享 9 折" :maxlength="60" />

        <div>
          <label class="block mb-1.5 text-xs font-medium text-gray-500">
            优惠类型 <span class="text-[#c17767]">*</span>
          </label>
          <div class="flex gap-2">
            <button
              v-for="opt in [
                { v: 'fixed', t: '固定金额' },
                { v: 'percent', t: '百分比折扣' },
              ]"
              :key="opt.v"
              type="button"
              class="flex-1 px-4 py-2.5 rounded-xl text-sm font-medium border transition-all duration-200"
              :class="
                form.type === opt.v
                  ? 'border-[#4a9d9a] bg-[#4a9d9a]/[0.07] text-[#4a9d9a]'
                  : 'border-gray-200 text-gray-500 hover:border-gray-300'
              "
              @click="form.type = opt.v as 'fixed' | 'percent'"
            >
              {{ opt.t }}
            </button>
          </div>
        </div>

        <div class="grid sm:grid-cols-2 gap-4">
          <WdInput
            v-if="form.type === 'fixed'"
            v-model="form.fixedYuan"
            type="number"
            label="优惠金额"
            required
            :min="0.01"
            :step="1"
            :hint="`单位：${symbol}（元）`"
          />
          <template v-else>
            <WdInput
              v-model="form.discount"
              type="number"
              label="折扣"
              required
              :min="0.1"
              :max="9.9"
              :step="0.5"
              hint="9 = 9 折，即优惠 10%"
            />
            <WdInput
              v-model="form.maxDiscountYuan"
              type="number"
              label="最大优惠"
              :min="0"
              :hint="`${symbol}，0 表示不限`"
            />
          </template>
          <WdInput
            v-model="form.minYuan"
            type="number"
            label="最低消费"
            :min="0"
            :hint="`${symbol}，0 表示不限`"
          />
        </div>

        <div>
          <label class="block mb-1.5 text-xs font-medium text-gray-500">
            适用范围 <span class="text-[#c17767]">*</span>
          </label>
          <div class="flex gap-2">
            <button
              v-for="opt in [
                { v: 'all', t: '全部商品' },
                { v: 'products', t: '指定商品' },
              ]"
              :key="opt.v"
              type="button"
              class="flex-1 px-4 py-2.5 rounded-xl text-sm font-medium border transition-all duration-200"
              :class="
                form.scope === opt.v
                  ? 'border-[#4a9d9a] bg-[#4a9d9a]/[0.07] text-[#4a9d9a]'
                  : 'border-gray-200 text-gray-500 hover:border-gray-300'
              "
              @click="form.scope = opt.v as 'all' | 'products'"
            >
              {{ opt.t }}
            </button>
          </div>
        </div>

        <div v-if="form.scope === 'products'">
          <label class="block mb-1.5 text-xs font-medium text-gray-500">
            指定商品 <span class="text-[#c17767]">*</span>
          </label>
          <div class="relative mb-2">
            <Search class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-300 z-10" />
            <input
              v-model="productKeyword"
              placeholder="搜索商品名称后回车"
              aria-label="搜索商品名称"
              class="w-full pl-9 pr-3.5 py-2.5 bg-white border border-gray-200 rounded-xl text-sm text-gray-800 placeholder:text-gray-300 focus:outline-none focus:ring-2 focus:ring-[#4a9d9a]/30 focus:border-[#4a9d9a] transition-all duration-200"
              @keyup.enter="searchProducts"
            />
          </div>
          <div class="max-h-52 overflow-y-auto rounded-xl border border-gray-200 divide-y divide-gray-50">
            <label
              v-for="p in products"
              :key="p.id"
              class="flex items-center gap-3 px-3.5 py-2.5 cursor-pointer hover:bg-[#faf8f5] transition-colors duration-200"
            >
              <input
                type="checkbox"
                class="accent-[#4a9d9a] shrink-0"
                :checked="form.product_ids.includes(p.id)"
                @change="toggleProduct(p.id)"
              />
              <span class="flex-1 text-sm text-gray-700 truncate">{{ p.name }}</span>
              <span class="text-xs text-gray-400 tabular shrink-0">
                {{ symbol }}{{ formatAmount(p.price) }}
              </span>
            </label>
            <p v-if="!products.length" class="px-3.5 py-6 text-center text-sm text-gray-400">
              没有搜索到商品
            </p>
          </div>
          <p class="mt-1.5 text-xs text-gray-400">
            已选 {{ form.product_ids.length }} 个商品。只有这些商品可以使用该券。
          </p>
        </div>

        <div class="grid sm:grid-cols-2 gap-4">
          <WdInput
            v-model="form.usage_limit"
            type="number"
            label="总使用次数"
            :min="0"
            hint="0 表示不限"
          />
          <WdInput
            v-model="form.per_user_limit"
            type="number"
            label="每人限用"
            :min="0"
            hint="按邮箱计算，0 表示不限"
          />
        </div>

        <div class="grid sm:grid-cols-2 gap-4">
          <WdInput v-model="form.start_at" type="date" label="开始日期" hint="留空表示不限" />
          <WdInput v-model="form.expire_at" type="date" label="结束日期" hint="留空表示永久有效" />
        </div>

        <div>
          <label class="block mb-1.5 text-xs font-medium text-gray-500">状态</label>
          <div class="flex gap-2">
            <button
              v-for="opt in [
                { v: 'active', t: '启用' },
                { v: 'disabled', t: '停用' },
              ]"
              :key="opt.v"
              type="button"
              class="flex-1 px-4 py-2.5 rounded-xl text-sm font-medium border transition-all duration-200"
              :class="
                form.status === opt.v
                  ? 'border-[#4a9d9a] bg-[#4a9d9a]/[0.07] text-[#4a9d9a]'
                  : 'border-gray-200 text-gray-500 hover:border-gray-300'
              "
              @click="form.status = opt.v as 'active' | 'disabled'"
            >
              {{ opt.t }}
            </button>
          </div>
        </div>
      </div>

      <template #footer>
        <WdButton @click="dialogVisible = false">取消</WdButton>
        <WdButton variant="primary" :loading="submitting" @click="submit">保存</WdButton>
      </template>
    </WdModal>

    <!-- 核销记录 -->
    <WdModal v-model="usageVisible" :title="`核销记录 · ${usageCoupon?.code}`" width="620px">
      <div class="rounded-xl border border-gray-100 overflow-hidden">
        <table class="w-full">
          <thead class="bg-[#faf8f5]">
            <tr>
              <th class="text-left py-2.5 px-3 text-xs font-medium text-gray-400">订单号</th>
              <th class="text-left py-2.5 px-3 text-xs font-medium text-gray-400">买家</th>
              <th class="text-right py-2.5 px-3 text-xs font-medium text-gray-400">优惠</th>
              <th class="text-left py-2.5 px-3 text-xs font-medium text-gray-400">时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="u in usages" :key="u.id" class="border-t border-gray-50">
              <td class="py-2.5 px-3 font-mono text-xs text-gray-600">{{ u.order_no }}</td>
              <td class="py-2.5 px-3 text-sm text-gray-500">{{ u.email }}</td>
              <td class="py-2.5 px-3 text-sm text-gray-700 text-right tabular">
                {{ symbol }}{{ formatAmount(u.discount_amount) }}
              </td>
              <td class="py-2.5 px-3 text-xs text-gray-400 tabular">
                {{ String(u.created_at).slice(0, 19).replace('T', ' ') }}
              </td>
            </tr>
            <tr v-if="!usages.length">
              <td colspan="4" class="py-10 text-center text-sm text-gray-400">暂无核销记录</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p class="mt-3 text-xs text-gray-400">
        共 {{ usageTotal }} 条记录（最多展示 50 条）。买家邮箱已脱敏。
      </p>
    </WdModal>
  </div>
</template>
