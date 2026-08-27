<script setup lang="ts">
import { computed } from 'vue'

/**
 * 状态徽章。
 * 模板 token：inline-flex px-2.5 py-1 text-xs font-medium rounded-lg bg-X/10 text-X
 */
const props = withDefaults(
  defineProps<{
    tone?: 'teal' | 'amber' | 'clay' | 'slate' | 'gray'
    dot?: boolean
  }>(),
  { tone: 'gray' },
)

const cls = computed(() => {
  switch (props.tone) {
    case 'teal':
      return 'bg-[#4a9d9a]/10 text-[#4a9d9a]'
    case 'amber':
      return 'bg-[#e8b86d]/15 text-[#b8873f]'
    case 'clay':
      return 'bg-[#c17767]/10 text-[#c17767]'
    case 'slate':
      return 'bg-[#6b8e8e]/10 text-[#6b8e8e]'
    default:
      return 'bg-gray-100 text-gray-400'
  }
})

const dotCls = computed(() => {
  switch (props.tone) {
    case 'teal':
      return 'bg-[#4a9d9a]'
    case 'amber':
      return 'bg-[#e8b86d]'
    case 'clay':
      return 'bg-[#c17767]'
    case 'slate':
      return 'bg-[#6b8e8e]'
    default:
      return 'bg-gray-300'
  }
})
</script>

<template>
  <span
    class="inline-flex items-center gap-1.5 px-2.5 py-1 text-xs font-medium rounded-lg whitespace-nowrap"
    :class="cls"
  >
    <span v-if="dot" class="w-1.5 h-1.5 rounded-full shrink-0" :class="dotCls" />
    <slot />
  </span>
</template>
