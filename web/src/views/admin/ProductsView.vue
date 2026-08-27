<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { KeyRound, Plus, Search, Upload } from 'lucide-vue-next'
import { ApiError, adminApi } from '@/api'
import type { Category, CustomField, Product } from '@/api/types'
import { formatAmount, formatStock, parseAmount } from '@/utils/format'
import { useShopStore } from '@/stores/shop'
import {
  WdBadge,
  WdButton,
  WdCard,
  WdInput,
  WdModal,
  WdPagination,
  WdSwitch,
  WdTable,
  confirmDialog,
  toast,
  type Column,
} from '@/ui'

const router = useRouter()
const shop = useShopStore()

const list = ref<Product[]>([])
const categories = ref<Category[]>([])
const total = ref(0)
const loading = ref(false)

const query = reactive({
  keyword: '',
  category_id: '' as string | number,
  status: '',
  delivery_type: '',
  page: 1,
  page_size: 20,
})

const dialogVisible = ref(false)
const editing = ref<Product | null>(null)
const submitting = ref(false)
const uploading = ref(false)

/** 表单里价格用「元」，提交前转成「分」。换算集中在 toPayload / openEdit */
const form = reactive({
  category_id: 0 as string | number,
  name: '',
  slug: '',
  cover: '',
  summary: '',
  description: '',
  priceYuan: 0 as number | null,
  originalYuan: 0 as number | null,
  stock: 0 as number | null,
  unlimitedStock: false,
  delivery_type: 'auto' as 'auto' | 'manual',
  status: 'off' as 'on' | 'off',
  sort: 0 as number | null,
  is_recommend: false,
  min_quantity: 1 as number | null,
  max_quantity: 100 as number | null,
  low_stock_threshold: 0 as number | null,
  custom_fields: [] as CustomField[],
})

/** 新增一个买家需填写的字段。key 用序号占位，保存前会校验格式。 */
function addCustomField() {
  if (form.custom_fields.length >= 5) {
    toast.error('最多 5 个自定义字段')
    return
  }
  form.custom_fields.push({
    key: `field${form.custom_fields.length + 1}`,
    label: '',
    type: 'text',
    required: true,
    placeholder: '',
    options: [],
  })
}

function removeCustomField(i: number) {
  form.custom_fields.splice(i, 1)
}

/** select 类型的选项在界面上按逗号分隔编辑，比做成动态列表省事得多 */
function optionsText(f: CustomField): string {
  return (f.options ?? []).join(',')
}
function setOptions(f: CustomField, v: string) {
  f.options = v
    .split(/[,，]/)
    .map((x) => x.trim())
    .filter(Boolean)
}

const stockDialog = ref(false)
const stockTarget = ref<Product | null>(null)
const stockValue = ref<number | null>(0)
const stockUnlimited = ref(false)

const symbol = computed(() => shop.symbol())

const columns: Column[] = [
  { key: 'name', label: '商品' },
  { key: 'category_name', label: '分类', width: '110px', hideOnMobile: true },
  { key: 'price', label: '价格', width: '130px', align: 'right' },
  { key: 'delivery_type', label: '发货', width: '90px', align: 'center' },
  { key: 'available_stock', label: '库存', width: '100px', align: 'center' },
  { key: 'sales_count', label: '销量', width: '80px', align: 'center', hideOnMobile: true },
  { key: 'status', label: '上架', width: '80px', align: 'center' },
  { key: 'actions', label: '操作', width: '160px', align: 'center' },
]

async function load() {
  loading.value = true
  try {
    const res = await adminApi.products({
      keyword: query.keyword || undefined,
      category_id: query.category_id || undefined,
      status: query.status || undefined,
      delivery_type: query.delivery_type || undefined,
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

async function loadCategories() {
  try {
    categories.value = await adminApi.categories()
  } catch {
    categories.value = []
  }
}

function search() {
  query.page = 1
  load()
}

function resetQuery() {
  Object.assign(query, { keyword: '', category_id: '', status: '', delivery_type: '', page: 1 })
  load()
}

function openCreate() {
  if (!categories.value.length) {
    toast.warning('请先创建至少一个商品分类')
    router.push({ name: 'admin-categories' })
    return
  }
  editing.value = null
  Object.assign(form, {
    category_id: categories.value[0].id,
    name: '',
    slug: '',
    cover: '',
    summary: '',
    description: '',
    priceYuan: 0,
    originalYuan: 0,
    stock: 0,
    unlimitedStock: false,
    delivery_type: 'auto',
    status: 'off',
    sort: 0,
    is_recommend: false,
    min_quantity: 1,
    max_quantity: 100,
    low_stock_threshold: 0,
    custom_fields: [],
  })
  dialogVisible.value = true
}

function openEdit(row: Product) {
  editing.value = row
  Object.assign(form, {
    category_id: row.category_id,
    name: row.name,
    slug: row.slug,
    cover: row.cover,
    summary: row.summary,
    description: row.description,
    priceYuan: row.price / 100,
    originalYuan: row.original_price / 100,
    stock: row.stock < 0 ? 0 : row.stock,
    unlimitedStock: row.stock < 0,
    delivery_type: row.delivery_type,
    status: row.status,
    sort: row.sort,
    is_recommend: row.is_recommend,
    min_quantity: row.min_quantity || 1,
    max_quantity: row.max_quantity || 100,
    low_stock_threshold: row.low_stock_threshold || 0,
    // 深拷贝：直接引用会让「取消」之后原对象也被改掉
    custom_fields: JSON.parse(JSON.stringify(row.custom_fields ?? [])),
  })
  dialogVisible.value = true
}

function toPayload() {
  return {
    category_id: Number(form.category_id),
    name: form.name.trim(),
    slug: form.slug.trim(),
    cover: form.cover,
    summary: form.summary,
    description: form.description,
    price: parseAmount(form.priceYuan ?? 0),
    original_price: parseAmount(form.originalYuan ?? 0),
    // 自动发货商品的库存由卡密数量决定，这里固定传 0
    stock:
      form.delivery_type === 'auto' ? 0 : form.unlimitedStock ? -1 : Math.max(0, form.stock ?? 0),
    delivery_type: form.delivery_type,
    status: form.status,
    sort: form.sort ?? 0,
    is_recommend: form.is_recommend,
    min_quantity: form.min_quantity ?? 1,
    max_quantity: form.max_quantity ?? 100,
    low_stock_threshold: Math.max(0, form.low_stock_threshold ?? 0),
    custom_fields: form.custom_fields,
  }
}

async function submit() {
  if (!form.name.trim()) {
    toast.error('请输入商品名称')
    return
  }
  if (parseAmount(form.priceYuan ?? 0) <= 0) {
    toast.error('商品价格必须大于 0')
    return
  }
  if ((form.min_quantity ?? 1) > (form.max_quantity ?? 100)) {
    toast.error('最小购买数量不能大于最大购买数量')
    return
  }

  submitting.value = true
  try {
    if (editing.value) {
      await adminApi.updateProduct(editing.value.id, toPayload())
      toast.success('商品已更新')
      dialogVisible.value = false
    } else {
      const p = await adminApi.createProduct(toPayload())
      toast.success('商品已创建')
      dialogVisible.value = false
      if (p.delivery_type === 'auto') {
        const go = await confirmDialog({
          title: '导入卡密',
          message: '这是自动发货商品，需要导入卡密后才有库存。现在去导入吗？',
          confirmText: '去导入',
          cancelText: '稍后',
        })
        if (go) {
          router.push({ name: 'admin-codes', query: { product_id: String(p.id) } })
          return
        }
      }
    }
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    submitting.value = false
  }
}

async function toggleStatus(row: Product) {
  const next = row.status === 'on' ? 'off' : 'on'
  try {
    await adminApi.setProductStatus(row.id, next)
    row.status = next
    toast.success(next === 'on' ? '商品已上架' : '商品已下架')
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '操作失败')
    await load()
  }
}

async function remove(row: Product) {
  const ok = await confirmDialog({
    title: '删除商品',
    message: `确定删除商品「${row.name}」吗？\n删除后商品不再展示，但历史订单不受影响（订单已保存商品快照）。`,
    confirmText: '删除',
    tone: 'danger',
  })
  if (!ok) return
  try {
    await adminApi.deleteProduct(row.id)
    toast.success('商品已删除')
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '删除失败')
  }
}

function openStock(row: Product) {
  if (row.delivery_type === 'auto') {
    router.push({ name: 'admin-codes', query: { product_id: String(row.id) } })
    return
  }
  stockTarget.value = row
  stockUnlimited.value = row.stock < 0
  stockValue.value = row.stock < 0 ? 0 : row.stock
  stockDialog.value = true
}

async function saveStock() {
  if (!stockTarget.value) return
  try {
    await adminApi.setProductStock(
      stockTarget.value.id,
      stockUnlimited.value ? -1 : Math.max(0, stockValue.value ?? 0),
    )
    toast.success('库存已更新')
    stockDialog.value = false
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '更新失败')
  }
}

async function onCoverPick(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  uploading.value = true
  try {
    const res = await adminApi.upload(file)
    form.cover = res.url
    toast.success('图片已上传')
  } catch (err) {
    toast.error(err instanceof ApiError ? err.message : '上传失败')
  } finally {
    uploading.value = false
    input.value = ''
  }
}

const onSaleCount = computed(() => list.value.filter((p) => p.status === 'on').length)

onMounted(() => {
  loadCategories()
  load()
})
</script>

<template>
  <div class="space-y-5">
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
      <p class="text-xs text-gray-500">
        共 <span class="tabular text-gray-700">{{ total }}</span> 个商品，本页
        <span class="tabular text-gray-700">{{ onSaleCount }}</span> 个在售
      </p>
      <WdButton variant="primary" @click="openCreate">
        <Plus class="w-4 h-4" />
        新增商品
      </WdButton>
    </div>

    <WdCard flush>
      <div class="px-6 py-5">
        <!-- 筛选 -->
        <div class="flex flex-wrap items-end gap-3 mb-5">
          <div class="relative w-52">
            <Search class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-300 z-10" />
            <input
              v-model="query.keyword"
              placeholder="搜索商品名称 / 别名"
              aria-label="搜索商品名称或别名"
              class="w-full pl-9 pr-3.5 py-2.5 bg-white border border-gray-200 rounded-xl text-sm text-gray-800 placeholder:text-gray-300 focus:outline-none focus:ring-2 focus:ring-[#4a9d9a]/30 focus:border-[#4a9d9a] transition-all duration-200"
              @keyup.enter="search"
            />
          </div>
          <WdInput
            v-model="query.category_id"
            type="select"
            placeholder="全部分类"
            clearable
            class="w-40"
            :options="categories.map((c) => ({ label: c.name, value: c.id }))"
            @change="search"
          />
          <WdInput
            v-model="query.status"
            type="select"
            placeholder="全部状态"
            clearable
            class="w-32"
            :options="[
              { label: '已上架', value: 'on' },
              { label: '已下架', value: 'off' },
            ]"
            @change="search"
          />
          <WdInput
            v-model="query.delivery_type"
            type="select"
            placeholder="发货方式"
            clearable
            class="w-36"
            :options="[
              { label: '自动发货', value: 'auto' },
              { label: '手动发货', value: 'manual' },
            ]"
            @change="search"
          />
          <WdButton variant="primary" @click="search">查询</WdButton>
          <WdButton @click="resetQuery">重置</WdButton>
        </div>

        <WdTable :columns="columns" :rows="list" :loading="loading" empty-text="没有找到商品">
          <template #name="{ row }">
            <div class="flex items-center gap-3">
              <img
                v-if="row.cover"
                :src="row.cover"
                :alt="row.name"
                class="w-10 h-10 rounded-lg object-cover shrink-0"
              />
              <span
                v-else
                class="w-10 h-10 rounded-lg bg-[#faf8f5] grid place-items-center shrink-0 text-sm text-gray-300"
              >
                {{ row.name.slice(0, 1) }}
              </span>
              <div class="min-w-0">
                <p class="text-sm font-medium text-gray-800 line-clamp-1 max-w-56">
                  {{ row.name }}
                </p>
                <p class="font-mono text-[11px] text-gray-300">{{ row.slug }}</p>
              </div>
            </div>
          </template>

          <template #price="{ row }">
            <p class="text-sm font-medium text-gray-800 tabular">
              {{ symbol }}{{ formatAmount(row.price) }}
            </p>
            <p
              v-if="row.original_price > row.price"
              class="text-[11px] text-gray-300 line-through tabular"
            >
              {{ symbol }}{{ formatAmount(row.original_price) }}
            </p>
          </template>

          <template #delivery_type="{ row }">
            <WdBadge :tone="row.delivery_type === 'auto' ? 'teal' : 'amber'">
              {{ row.delivery_type === 'auto' ? '自动' : '手动' }}
            </WdBadge>
          </template>

          <template #available_stock="{ row }">
            <button
              class="text-sm font-medium hover:underline tabular"
              :class="row.available_stock === 0 ? 'text-[#c17767]' : 'text-[#4a9d9a]'"
              @click="openStock(row as Product)"
            >
              {{ formatStock(row.available_stock) }}
            </button>
          </template>

          <template #sales_count="{ row }">
            <span class="tabular text-gray-500">{{ row.sales_count }}</span>
          </template>

          <template #status="{ row }">
            <WdSwitch
              :model-value="row.status === 'on'"
              :label="`${row.name} 上架状态`"
              @update:model-value="toggleStatus(row as Product)"
            />
          </template>

          <template #actions="{ row }">
            <div class="flex items-center justify-center gap-3">
              <button
                class="text-xs font-medium text-[#4a9d9a] hover:underline"
                @click="openEdit(row as Product)"
              >
                编辑
              </button>
              <button
                v-if="row.delivery_type === 'auto'"
                class="text-xs font-medium text-[#6b8e8e] hover:underline"
                @click="router.push({ name: 'admin-codes', query: { product_id: String(row.id) } })"
              >
                卡密
              </button>
              <button
                class="text-xs font-medium text-[#c17767] hover:underline"
                @click="remove(row as Product)"
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

    <!-- 商品编辑 -->
    <WdModal
      v-model="dialogVisible"
      :title="editing ? '编辑商品' : '新增商品'"
      width="720px"
      :close-on-overlay="false"
    >
      <div class="space-y-4">
        <WdInput v-model="form.name" label="商品名称" required :maxlength="120" />

        <div class="grid sm:grid-cols-2 gap-4">
          <WdInput
            v-model="form.category_id"
            type="select"
            label="所属分类"
            required
            :options="categories.map((c) => ({ label: c.name, value: c.id }))"
          />
          <WdInput
            v-model="form.slug"
            label="URL 别名"
            placeholder="留空按商品名的汉语拼音生成"
            :hint="
              form.slug
                ? `商品页链接：/product/${form.slug}`
                : '留空则按汉语拼音生成，如「王者荣耀」→ wang-zhe-rong-yao；重名会自动加序号'
            "
          />
        </div>

        <div>
          <label class="block mb-1.5 text-xs font-medium text-gray-500">封面图</label>
          <div class="flex items-center gap-3">
            <label
              class="shrink-0 inline-flex items-center gap-2 px-4 py-2.5 text-sm font-medium text-gray-600 bg-white border border-gray-200 rounded-xl cursor-pointer hover:border-[#4a9d9a]/40 hover:text-[#4a9d9a] transition-all duration-200"
            >
              <Upload class="w-4 h-4" />
              {{ uploading ? '上传中…' : '上传' }}
              <input
                type="file"
                class="hidden"
                accept="image/jpeg,image/png,image/gif,image/webp"
                @change="onCoverPick"
              />
            </label>
            <input
              v-model="form.cover"
              placeholder="或直接填写图片 URL"
              aria-label="封面图 URL"
              class="flex-1 px-3.5 py-2.5 bg-white border border-gray-200 rounded-xl text-sm text-gray-800 placeholder:text-gray-300 focus:outline-none focus:ring-2 focus:ring-[#4a9d9a]/30 focus:border-[#4a9d9a] transition-all duration-200"
            />
            <img
              v-if="form.cover"
              :src="form.cover"
              alt="封面预览"
              class="w-10 h-10 rounded-lg object-cover border border-gray-200 shrink-0"
            />
          </div>
          <p class="mt-1.5 text-xs text-gray-400">支持 JPG / PNG / GIF / WebP，最大 5MB</p>
        </div>

        <WdInput
          v-model="form.summary"
          label="简介"
          :maxlength="150"
          placeholder="一句话卖点，展示在商品卡片上"
        />

        <div class="grid sm:grid-cols-2 gap-4">
          <WdInput
            v-model="form.priceYuan"
            type="number"
            label="售价"
            required
            :min="0"
            :step="1"
            :hint="`单位：${symbol}（元）`"
          />
          <WdInput
            v-model="form.originalYuan"
            type="number"
            label="划线价"
            :min="0"
            :step="1"
            hint="高于售价时显示删除线原价"
          />
        </div>

        <div>
          <label class="block mb-1.5 text-xs font-medium text-gray-500">
            发货方式 <span class="text-[#c17767]">*</span>
          </label>
          <div class="flex gap-2">
            <button
              v-for="opt in [
                { v: 'auto', t: '自动发货（卡密）' },
                { v: 'manual', t: '手动发货' },
              ]"
              :key="opt.v"
              type="button"
              :disabled="!!editing"
              class="flex-1 px-4 py-2.5 rounded-xl text-sm font-medium border transition-all duration-200"
              :class="[
                form.delivery_type === opt.v
                  ? 'border-[#4a9d9a] bg-[#4a9d9a]/[0.07] text-[#4a9d9a]'
                  : 'border-gray-200 text-gray-500 hover:border-gray-300',
                editing && 'opacity-55 cursor-not-allowed',
              ]"
              @click="form.delivery_type = opt.v as 'auto' | 'manual'"
            >
              {{ opt.t }}
            </button>
          </div>
          <p class="mt-1.5 text-xs text-gray-400 leading-relaxed">
            <template v-if="form.delivery_type === 'auto'">
              付款成功后系统自动从卡密库中分配并发送给买家。库存 = 未使用卡密数量。
            </template>
            <template v-else>
              付款成功后订单进入「待发货」，需要管理员在订单页填写发货内容。
            </template>
            <template v-if="editing"><br />已创建的商品不允许更改发货方式。</template>
          </p>
        </div>

        <div v-if="form.delivery_type === 'manual'">
          <label class="block mb-1.5 text-xs font-medium text-gray-500">库存</label>
          <div class="flex items-center gap-4">
            <label class="flex items-center gap-2 text-sm text-gray-600 cursor-pointer">
              <input v-model="form.unlimitedStock" type="checkbox" class="accent-[#4a9d9a]" />
              无限库存
            </label>
            <input
              v-if="!form.unlimitedStock"
              v-model.number="form.stock"
              type="number"
              min="0"
              aria-label="库存数量"
              class="w-40 px-3.5 py-2.5 bg-white border border-gray-200 rounded-xl text-sm text-gray-800 focus:outline-none focus:ring-2 focus:ring-[#4a9d9a]/30 focus:border-[#4a9d9a] transition-all duration-200"
            />
          </div>
        </div>
        <p
          v-else
          class="px-3.5 py-2.5 rounded-xl bg-[#faf8f5] text-xs text-gray-500 leading-relaxed"
        >
          自动发货商品的库存由卡密数量决定，请在「卡密管理」中导入。
        </p>

        <div class="grid grid-cols-3 gap-4">
          <WdInput v-model="form.min_quantity" type="number" label="最少购买" :min="1" />
          <WdInput v-model="form.max_quantity" type="number" label="最多购买" :min="1" />
          <WdInput v-model="form.sort" type="number" label="排序" :min="0" />
          <WdInput
            v-model="form.low_stock_threshold"
            type="number"
            label="库存告警阈值"
            :min="0"
            hint="0 = 用全局设置"
          />
        </div>

        <!-- 买家需填写的信息：代充、账号租赁这类商品离不开它 -->
        <div class="pt-1">
          <div class="flex items-center justify-between gap-4 mb-2">
            <div class="min-w-0">
              <p class="text-sm font-medium text-gray-800">下单时需要买家填写的信息</p>
              <p class="mt-0.5 text-xs text-gray-500 leading-relaxed">
                例如代充商品需要买家的游戏账号 / 大区。留空则下单页不显示额外输入框。
              </p>
            </div>
            <WdButton size="sm" :disabled="form.custom_fields.length >= 5" @click="addCustomField">
              <Plus class="w-3.5 h-3.5" />
              添加字段
            </WdButton>
          </div>

          <div
            v-for="(f, i) in form.custom_fields"
            :key="i"
            class="mt-3 p-4 rounded-xl bg-[#faf8f5] space-y-3"
          >
            <div class="flex items-center justify-between gap-3">
              <span class="text-xs font-medium text-gray-500">字段 {{ i + 1 }}</span>
              <button
                class="text-xs font-medium text-[#c17767] hover:underline"
                @click="removeCustomField(i)"
              >
                删除
              </button>
            </div>

            <div class="grid sm:grid-cols-2 gap-3">
              <WdInput v-model="f.label" label="显示名称" required placeholder="如：游戏账号" />
              <WdInput
                v-model="f.key"
                label="字段标识"
                required
                placeholder="game_id"
                hint="英文字母开头，仅字母数字下划线"
              />
              <WdInput
                v-model="f.type"
                type="select"
                label="输入类型"
                :options="[
                  { label: '单行文本', value: 'text' },
                  { label: '多行文本', value: 'textarea' },
                  { label: '下拉选择', value: 'select' },
                ]"
              />
              <WdInput v-model="f.placeholder" label="输入提示" placeholder="可选" />
              <WdInput
                v-if="f.type === 'select'"
                class="sm:col-span-2"
                :model-value="optionsText(f)"
                label="可选项"
                required
                placeholder="微信区,QQ区"
                hint="用逗号分隔"
                @update:model-value="(v) => setOptions(f, String(v ?? ''))"
              />
              <WdInput
                v-if="f.type !== 'select'"
                class="sm:col-span-2"
                v-model="f.pattern"
                label="格式校验（正则，可选）"
                placeholder="^[0-9]{6,12}$"
                mono
                hint="留空表示不校验。填错会导致买家无法下单，请先自行验证"
              />
            </div>

            <label class="flex items-center gap-2 text-sm text-gray-600 cursor-pointer">
              <input v-model="f.required" type="checkbox" class="accent-[#4a9d9a]" />
              必填
            </label>
          </div>
        </div>

        <WdInput
          v-model="form.description"
          type="textarea"
          label="详情描述"
          :rows="7"
          placeholder="支持基础 HTML 标签（p / strong / ul / table / img / a 等）"
          hint="出于安全考虑，后端会对描述做 XSS 白名单净化，<script> 等标签会被自动移除"
        />

        <div class="flex flex-wrap items-center gap-6 pt-1">
          <label class="flex items-center gap-2 text-sm text-gray-600 cursor-pointer">
            <input v-model="form.is_recommend" type="checkbox" class="accent-[#4a9d9a]" />
            首页推荐
          </label>
          <label class="flex items-center gap-2.5 text-sm text-gray-600 cursor-pointer">
            <WdSwitch
              :model-value="form.status === 'on'"
              label="立即上架"
              @update:model-value="(v) => (form.status = v ? 'on' : 'off')"
            />
            {{ form.status === 'on' ? '立即上架' : '暂不上架' }}
          </label>
        </div>
      </div>

      <template #footer>
        <WdButton @click="dialogVisible = false">取消</WdButton>
        <WdButton variant="primary" :loading="submitting" @click="submit">保存</WdButton>
      </template>
    </WdModal>

    <!-- 库存 -->
    <WdModal v-model="stockDialog" title="修改库存" width="400px">
      <p class="text-sm text-gray-500">{{ stockTarget?.name }}</p>
      <div class="mt-5 flex items-center gap-4">
        <label class="flex items-center gap-2 text-sm text-gray-600 cursor-pointer">
          <input v-model="stockUnlimited" type="checkbox" class="accent-[#4a9d9a]" />
          无限库存
        </label>
        <input
          v-if="!stockUnlimited"
          v-model.number="stockValue"
          type="number"
          min="0"
          aria-label="库存数量"
          class="w-40 px-3.5 py-2.5 bg-white border border-gray-200 rounded-xl text-sm text-gray-800 focus:outline-none focus:ring-2 focus:ring-[#4a9d9a]/30 focus:border-[#4a9d9a] transition-all duration-200"
        />
      </div>
      <template #footer>
        <WdButton @click="stockDialog = false">取消</WdButton>
        <WdButton variant="primary" @click="saveStock">
          <KeyRound class="w-4 h-4" />
          保存
        </WdButton>
      </template>
    </WdModal>
  </div>
</template>
