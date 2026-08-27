<script setup lang="ts">
import { computed } from 'vue'

/**
 * 暖色仪表盘按钮。
 *
 * 悬停用「轻微上浮 + 加深投影」，与模板一致；
 * 加载中会禁用并展示旋转指示，避免重复提交。
 */
const props = withDefaults(
  defineProps<{
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger' | 'warning'
    /** lg 用于前台的主行动按钮（立即购买、提交订单） */
    size?: 'sm' | 'md' | 'lg'
    loading?: boolean
    disabled?: boolean
    block?: boolean
    type?: 'button' | 'submit'
  }>(),
  { variant: 'secondary', size: 'md', type: 'button' },
)

const isDisabled = computed(() => props.disabled || props.loading)

const sizeCls = computed(() => {
  switch (props.size) {
    case 'sm':
      return 'px-3 py-1.5 text-xs gap-1.5'
    case 'lg':
      return 'px-6 py-3.5 text-base gap-2.5'
    default:
      return 'px-5 py-2.5 text-sm gap-2'
  }
})

const variantCls = computed(() => {
  switch (props.variant) {
    case 'primary':
      return 'bg-[#4a9d9a] text-white shadow-lg shadow-[#4a9d9a]/25 hover:shadow-xl'
    case 'danger':
      return 'bg-[#c17767] text-white shadow-lg shadow-[#c17767]/25 hover:shadow-xl'
    case 'warning':
      return 'bg-[#e8b86d] text-white shadow-lg shadow-[#e8b86d]/25 hover:shadow-xl'
    case 'ghost':
      return 'text-gray-500 hover:text-gray-800 hover:bg-white hover:shadow-sm'
    default:
      return 'bg-white text-gray-600 border border-gray-200 hover:border-[#4a9d9a]/40 hover:text-[#4a9d9a] hover:shadow-md'
  }
})
</script>

<template>
  <button
    :type="type"
    :disabled="isDisabled"
    class="inline-flex items-center justify-center font-medium rounded-xl transition-all duration-200 whitespace-nowrap"
    :class="[
      sizeCls,
      variantCls,
      block && 'w-full',
      isDisabled ? 'opacity-55 cursor-not-allowed' : 'hover:-translate-y-0.5 active:translate-y-0',
    ]"
  >
    <svg
      v-if="loading"
      class="w-3.5 h-3.5 animate-spin shrink-0"
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="3" opacity="0.25" />
      <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" stroke-width="3" stroke-linecap="round" />
    </svg>
    <slot />
  </button>
</template>
