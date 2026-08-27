<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Plus } from 'lucide-vue-next'
import { ApiError, adminApi } from '@/api'
import type { Category } from '@/api/types'
import {
  WdBadge,
  WdButton,
  WdCard,
  WdInput,
  WdModal,
  WdSwitch,
  WdTable,
  confirmDialog,
  toast,
  type Column,
} from '@/ui'

const list = ref<Category[]>([])
const loading = ref(false)

const dialogVisible = ref(false)
const editing = ref<Category | null>(null)
const submitting = ref(false)
const form = reactive({
  name: '',
  slug: '',
  description: '',
  icon: '',
  sort: 0,
  status: 'active' as 'active' | 'disabled',
})

// 分类下有商品时不能直接删，需要先转移
const moveVisible = ref(false)
const moveFrom = ref<Category | null>(null)
const moveTarget = ref<number>(0)

const columns: Column[] = [
  { key: 'sort', label: '排序', width: '80px', align: 'center' },
  { key: 'name', label: '名称' },
  { key: 'slug', label: '别名', hideOnMobile: true },
  { key: 'description', label: '描述', hideOnMobile: true },
  { key: 'product_count', label: '商品数', width: '90px', align: 'center' },
  { key: 'status', label: '启用', width: '80px', align: 'center' },
  { key: 'actions', label: '操作', width: '130px', align: 'center' },
]

async function load() {
  loading.value = true
  try {
    list.value = await adminApi.categories()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = null
  Object.assign(form, { name: '', slug: '', description: '', icon: '', sort: 0, status: 'active' })
  dialogVisible.value = true
}

function openEdit(row: Category) {
  editing.value = row
  Object.assign(form, {
    name: row.name,
    slug: row.slug,
    description: row.description,
    icon: row.icon,
    sort: row.sort,
    status: row.status,
  })
  dialogVisible.value = true
}

async function submit() {
  if (!form.name.trim()) {
    toast.error('请输入分类名称')
    return
  }
  submitting.value = true
  try {
    if (editing.value) {
      await adminApi.updateCategory(editing.value.id, { ...form })
      toast.success('分类已更新')
    } else {
      await adminApi.createCategory({ ...form })
      toast.success('分类已创建')
    }
    dialogVisible.value = false
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    submitting.value = false
  }
}

async function remove(row: Category) {
  if ((row.product_count ?? 0) > 0) {
    // 直接引导去转移，而不是让用户点了删除再看到报错
    moveFrom.value = row
    moveTarget.value = list.value.find((c) => c.id !== row.id)?.id ?? 0
    moveVisible.value = true
    return
  }
  const ok = await confirmDialog({
    title: '删除分类',
    message: `确定删除分类「${row.name}」吗？`,
    confirmText: '删除',
    tone: 'danger',
  })
  if (!ok) return

  try {
    await adminApi.deleteCategory(row.id)
    toast.success('分类已删除')
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '删除失败')
  }
}

async function doMove() {
  if (!moveFrom.value || !moveTarget.value) {
    toast.error('请选择目标分类')
    return
  }
  try {
    const res = await adminApi.moveCategoryProducts(moveFrom.value.id, moveTarget.value)
    toast.success(`已转移 ${res.moved} 个商品，现在可以删除该分类了`)
    moveVisible.value = false
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '转移失败')
  }
}

async function toggleStatus(row: Category) {
  const next = row.status === 'active' ? 'disabled' : 'active'
  try {
    await adminApi.updateCategory(row.id, { ...row, status: next })
    row.status = next
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '操作失败')
    await load()
  }
}

const enabledCount = computed(() => list.value.filter((c) => c.status === 'active').length)
const productCount = computed(() => list.value.reduce((n, c) => n + (c.product_count ?? 0), 0))

onMounted(load)
</script>

<template>
  <div class="space-y-5">
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
      <p class="text-xs text-gray-500">
        共 <span class="tabular text-gray-700">{{ list.length }}</span> 个分类，
        <span class="tabular text-gray-700">{{ enabledCount }}</span> 个启用中，
        关联 <span class="tabular text-gray-700">{{ productCount }}</span> 个商品
      </p>
      <WdButton variant="primary" @click="openCreate">
        <Plus class="w-4 h-4" />
        新增分类
      </WdButton>
    </div>

    <p class="px-5 py-3.5 rounded-2xl bg-white shadow-xl shadow-black/[0.04] text-xs text-gray-500 leading-relaxed">
      分类下存在商品时不允许删除。需要删除请先把商品转移到其他分类 —— 否则商品会变成「孤儿」，
      前台筛选与后台管理都会出现异常数据。
    </p>

    <WdCard flush>
      <div class="px-6 py-5">
        <WdTable
          :columns="columns"
          :rows="list"
          :loading="loading"
          empty-text="还没有分类，先创建一个吧"
        >
          <template #sort="{ row }">
            <span class="tabular text-gray-400">{{ row.sort }}</span>
          </template>

          <template #name="{ row }">
            <span class="text-sm font-medium text-gray-800">
              <span v-if="row.icon" class="mr-1.5">{{ row.icon }}</span>{{ row.name }}
            </span>
          </template>

          <template #slug="{ row }">
            <span class="font-mono text-xs text-gray-400">{{ row.slug }}</span>
          </template>

          <template #description="{ row }">
            <span class="text-sm text-gray-500 line-clamp-1 max-w-64">
              {{ row.description || '—' }}
            </span>
          </template>

          <template #product_count="{ row }">
            <WdBadge v-if="row.product_count" tone="teal">{{ row.product_count }}</WdBadge>
            <span v-else class="text-gray-300">0</span>
          </template>

          <template #status="{ row }">
            <WdSwitch
              :model-value="row.status === 'active'"
              :label="`${row.name} 启用状态`"
              @update:model-value="toggleStatus(row as Category)"
            />
          </template>

          <template #actions="{ row }">
            <div class="flex items-center justify-center gap-3">
              <button
                class="text-xs font-medium text-[#4a9d9a] hover:underline"
                @click="openEdit(row as Category)"
              >
                编辑
              </button>
              <button
                class="text-xs font-medium text-[#c17767] hover:underline"
                @click="remove(row as Category)"
              >
                删除
              </button>
            </div>
          </template>
        </WdTable>
      </div>
    </WdCard>

    <!-- 编辑 -->
    <WdModal
      v-model="dialogVisible"
      :title="editing ? '编辑分类' : '新增分类'"
      width="480px"
      :close-on-overlay="false"
    >
      <div class="space-y-4">
        <WdInput v-model="form.name" label="分类名称" required :maxlength="30" placeholder="如：软件、账号、游戏" />
        <WdInput
          v-model="form.slug"
          label="URL 别名"
          placeholder="留空按分类名的汉语拼音生成"
          hint="只能包含小写字母、数字和连字符。留空则按汉语拼音生成，如「游戏点卡」→ you-xi-dian-ka"
        />
        <WdInput v-model="form.icon" label="图标" placeholder="可填 emoji，如 💻" :maxlength="8" />
        <WdInput v-model="form.description" type="textarea" label="描述" :rows="2" :maxlength="200" />
        <div class="grid grid-cols-2 gap-4">
          <WdInput
            v-model="form.sort"
            type="number"
            label="排序"
            :min="0"
            :max="9999"
            hint="数值越大越靠前"
          />
          <div>
            <label class="block mb-1.5 text-xs font-medium text-gray-500">状态</label>
            <div class="flex items-center gap-3 h-[42px]">
              <WdSwitch
                :model-value="form.status === 'active'"
                label="启用分类"
                @update:model-value="(v) => (form.status = v ? 'active' : 'disabled')"
              />
              <span class="text-sm text-gray-500">
                {{ form.status === 'active' ? '启用' : '停用' }}
              </span>
            </div>
          </div>
        </div>
      </div>
      <template #footer>
        <WdButton @click="dialogVisible = false">取消</WdButton>
        <WdButton variant="primary" :loading="submitting" @click="submit">保存</WdButton>
      </template>
    </WdModal>

    <!-- 转移商品 -->
    <WdModal v-model="moveVisible" title="转移商品" width="420px">
      <p class="text-sm text-gray-500 leading-relaxed">
        分类「{{ moveFrom?.name }}」下还有
        <span class="font-medium text-gray-800">{{ moveFrom?.product_count }}</span> 个商品，
        需要先转移到其他分类才能删除。
      </p>
      <div class="mt-5">
        <WdInput
          v-model="moveTarget"
          type="select"
          label="目标分类"
          :options="
            list.filter((c) => c.id !== moveFrom?.id).map((c) => ({ label: c.name, value: c.id }))
          "
        />
      </div>
      <template #footer>
        <WdButton @click="moveVisible = false">取消</WdButton>
        <WdButton variant="primary" @click="doMove">确认转移</WdButton>
      </template>
    </WdModal>
  </div>
</template>
