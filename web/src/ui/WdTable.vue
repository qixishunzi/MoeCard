<script setup lang="ts" generic="T extends Record<string, any>">
import { Inbox } from 'lucide-vue-next'

export interface Column {
  key: string
  label: string
  /** 列宽，如 '120px'；不填则自适应 */
  width?: string
  align?: 'left' | 'center' | 'right'
  /** 小屏隐藏该列，避免横向挤压 */
  hideOnMobile?: boolean
}

withDefaults(
  defineProps<{
    columns: Column[]
    rows: T[]
    loading?: boolean
    rowKey?: string
    emptyText?: string
  }>(),
  { rowKey: 'id', emptyText: '暂无数据' },
)

const alignCls: Record<string, string> = {
  left: 'text-left',
  center: 'text-center',
  right: 'text-right',
}
</script>

<template>
  <div class="relative">
    <!-- 加载遮罩：不卸载表格内容，避免每次翻页整表闪烁 -->
    <div
      v-if="loading"
      class="absolute inset-0 z-10 bg-white/60 backdrop-blur-[1px] flex items-center justify-center rounded-xl"
    >
      <svg class="w-6 h-6 animate-spin text-[#4a9d9a]" viewBox="0 0 24 24" fill="none">
        <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="2.5" opacity="0.2" />
        <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" />
      </svg>
    </div>

    <div class="overflow-x-auto -mx-6 px-6">
      <table class="w-full min-w-max">
        <thead>
          <tr class="border-b border-gray-100">
            <th
              v-for="c in columns"
              :key="c.key"
              class="py-3 px-4 text-xs font-medium text-gray-500 tracking-wider whitespace-nowrap"
              :class="[alignCls[c.align ?? 'left'], c.hideOnMobile && 'hidden md:table-cell']"
              :style="c.width ? { width: c.width } : undefined"
            >
              {{ c.label }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(row, i) in rows"
            :key="String(row[rowKey] ?? i)"
            class="border-b border-gray-50 hover:bg-[#faf8f5] transition-colors duration-200"
          >
            <td
              v-for="c in columns"
              :key="c.key"
              class="py-4 px-4 text-sm text-gray-600 align-middle"
              :class="[alignCls[c.align ?? 'left'], c.hideOnMobile && 'hidden md:table-cell']"
            >
              <slot :name="c.key" :row="row" :index="i">{{ row[c.key] ?? '—' }}</slot>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="!rows.length && !loading" class="py-16 flex flex-col items-center gap-3">
      <span class="w-12 h-12 rounded-2xl bg-[#faf8f5] flex items-center justify-center">
        <Inbox class="w-5 h-5 text-gray-300" />
      </span>
      <p class="text-sm text-gray-500">
        <slot name="empty">{{ emptyText }}</slot>
      </p>
    </div>
  </div>
</template>
