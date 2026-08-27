<script setup lang="ts">
withDefaults(
  defineProps<{
    modelValue: string
    tabs: { value: string; label: string }[]
    /** pill 用于工具栏内的小切换，line 用于页面级分栏 */
    variant?: 'pill' | 'line'
    /**
     * 容器所在的底色。
     *
     * pill 的外框原来固定用 #faf8f5 —— 那正是后台页面的底色，
     * 于是整个外框在页面上完全隐形，只剩一个高亮的当前项，
     * 旁边的「邮件模板」看起来像一段普通文字而不是可点的按钮。
     * 这里按所处底色选不同外框：页面上用白底+描边，卡片内用灰底。
     */
    on?: 'page' | 'card'
  }>(),
  { variant: 'pill', on: 'page' },
)

const emit = defineEmits<{
  'update:modelValue': [v: string]
  change: [v: string]
}>()

function select(v: string) {
  emit('update:modelValue', v)
  emit('change', v)
}
</script>

<template>
  <!-- 药丸式 -->
  <div
    v-if="variant !== 'line'"
    class="inline-flex items-center gap-1 rounded-xl p-1"
    :class="
      on === 'card'
        ? 'bg-[#faf8f5] border border-gray-200'
        : 'bg-white border border-gray-200 shadow-sm'
    "
    role="tablist"
  >
    <button
      v-for="t in tabs"
      :key="t.value"
      type="button"
      role="tab"
      :aria-selected="modelValue === t.value"
      class="px-3.5 py-1.5 text-xs font-medium rounded-lg transition-all duration-200 whitespace-nowrap"
      :class="
        modelValue === t.value
          ? 'bg-[#4a9d9a] text-white shadow-md shadow-[#4a9d9a]/20'
          : 'text-gray-500 hover:text-gray-800 hover:bg-[#faf8f5]'
      "
      @click="select(t.value)"
    >
      {{ t.label }}
    </button>
  </div>

  <!-- 下划线式 -->
  <div
    v-else
    class="flex items-center gap-6 border-b border-gray-100 overflow-x-auto no-scrollbar"
    role="tablist"
  >
    <button
      v-for="t in tabs"
      :key="t.value"
      type="button"
      role="tab"
      :aria-selected="modelValue === t.value"
      class="relative pb-3 text-sm transition-colors duration-200 whitespace-nowrap"
      :class="
        modelValue === t.value ? 'text-gray-800 font-medium' : 'text-gray-500 hover:text-gray-700'
      "
      @click="select(t.value)"
    >
      {{ t.label }}
      <span
        v-if="modelValue === t.value"
        class="absolute left-0 right-0 -bottom-px h-0.5 rounded-full bg-[#4a9d9a]"
      />
    </button>
  </div>
</template>
