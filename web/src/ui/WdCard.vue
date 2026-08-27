<script setup lang="ts">
/**
 * 暖色仪表盘卡片。
 * 模板 token：bg-white rounded-2xl shadow-xl shadow-black/[0.04] p-6
 */
withDefaults(
  defineProps<{
    title?: string
    subtitle?: string
    /** 关闭内边距，用于表格等需要贴边的内容 */
    flush?: boolean
    hoverable?: boolean
  }>(),
  {},
)
</script>

<template>
  <section
    class="bg-white rounded-2xl shadow-xl shadow-black/[0.04] transition-all duration-300"
    :class="hoverable && 'hover:shadow-2xl hover:-translate-y-1'"
  >
    <header
      v-if="title || $slots.header || $slots.actions"
      class="flex flex-col sm:flex-row sm:items-center justify-between gap-3 px-6 pt-6"
      :class="!flush && 'pb-1'"
    >
      <div v-if="title || subtitle">
        <h2 class="text-lg font-semibold text-gray-800">{{ title }}</h2>
        <p v-if="subtitle" class="text-xs text-gray-500 mt-0.5">{{ subtitle }}</p>
      </div>
      <slot v-else name="header" />
      <div v-if="$slots.actions" class="flex items-center gap-2 flex-wrap">
        <slot name="actions" />
      </div>
    </header>

    <div :class="flush ? '' : 'p-6'">
      <slot />
    </div>
  </section>
</template>
