<script setup lang="ts">
import { computed } from 'vue'
import { ChevronLeft, ChevronRight } from 'lucide-vue-next'
import WdSelect from './WdSelect.vue'

const props = withDefaults(
  defineProps<{
    page: number
    pageSize: number
    total: number
    pageSizes?: number[]
  }>(),
  { pageSizes: () => [20, 50, 100] },
)

const emit = defineEmits<{
  'update:page': [v: number]
  'update:pageSize': [v: number]
  change: []
}>()

const totalPages = computed(() => Math.max(1, Math.ceil(props.total / props.pageSize)))

/** 页码窗口：始终显示首尾，中间围绕当前页，用 -1 表示省略号 */
const pages = computed<number[]>(() => {
  const tp = totalPages.value
  const cur = props.page
  if (tp <= 7) return Array.from({ length: tp }, (_, i) => i + 1)

  const out: number[] = [1]
  const from = Math.max(2, cur - 1)
  const to = Math.min(tp - 1, cur + 1)
  if (from > 2) out.push(-1)
  for (let i = from; i <= to; i++) out.push(i)
  if (to < tp - 1) out.push(-1)
  out.push(tp)
  return out
})

function go(p: number) {
  if (p < 1 || p > totalPages.value || p === props.page) return
  emit('update:page', p)
  emit('change')
}

function onSize(v: string | number | null) {
  emit('update:pageSize', Number(v))
  emit('update:page', 1)
  emit('change')
}
</script>

<template>
  <div
    v-if="total > 0"
    class="flex flex-col sm:flex-row items-center justify-between gap-3 pt-5 mt-1 border-t border-gray-50"
  >
    <div class="flex items-center gap-3 text-xs text-gray-500">
      <span>共 <span class="tabular text-gray-600">{{ total }}</span> 条</span>
      <WdSelect
        class="w-24"
        size="sm"
        :model-value="pageSize"
        :options="pageSizes.map((s) => ({ label: `${s} 条/页`, value: s }))"
        aria-label="每页条数"
        @update:model-value="onSize"
      />
    </div>

    <nav class="flex items-center gap-1" aria-label="分页">
      <button
        class="p-1.5 rounded-lg text-gray-400 hover:bg-[#faf8f5] hover:text-gray-700 disabled:opacity-40 disabled:cursor-not-allowed transition-all duration-200"
        :disabled="page <= 1"
        aria-label="上一页"
        @click="go(page - 1)"
      >
        <ChevronLeft class="w-4 h-4" />
      </button>

      <template v-for="(p, i) in pages" :key="`${p}-${i}`">
        <span v-if="p === -1" class="px-1.5 text-xs text-gray-300">…</span>
        <button
          v-else
          class="min-w-8 h-8 px-2 rounded-lg text-xs font-medium transition-all duration-200 tabular"
          :class="
            p === page
              ? 'bg-[#4a9d9a] text-white shadow-md shadow-[#4a9d9a]/20'
              : 'text-gray-500 hover:bg-[#faf8f5] hover:text-gray-800'
          "
          :aria-current="p === page ? 'page' : undefined"
          @click="go(p)"
        >
          {{ p }}
        </button>
      </template>

      <button
        class="p-1.5 rounded-lg text-gray-400 hover:bg-[#faf8f5] hover:text-gray-700 disabled:opacity-40 disabled:cursor-not-allowed transition-all duration-200"
        :disabled="page >= totalPages"
        aria-label="下一页"
        @click="go(page + 1)"
      >
        <ChevronRight class="w-4 h-4" />
      </button>
    </nav>
  </div>
</template>
