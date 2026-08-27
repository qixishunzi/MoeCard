<script setup lang="ts">
const props = withDefaults(
  defineProps<{
    modelValue: boolean
    disabled?: boolean
    label?: string
  }>(),
  {},
)

const emit = defineEmits<{ 'update:modelValue': [v: boolean] }>()

function toggle() {
  if (props.disabled) return
  emit('update:modelValue', !props.modelValue)
}
</script>

<template>
  <button
    type="button"
    role="switch"
    :aria-checked="modelValue"
    :aria-label="label"
    :disabled="disabled"
    class="relative inline-flex items-center h-6 w-11 rounded-full transition-colors duration-200 shrink-0 focus:outline-none focus-visible:ring-2 focus-visible:ring-[#4a9d9a]/40"
    :class="[modelValue ? 'bg-[#4a9d9a]' : 'bg-gray-200', disabled && 'opacity-50 cursor-not-allowed']"
    @click="toggle"
  >
    <span
      class="inline-block w-4.5 h-4.5 bg-white rounded-full shadow-sm transition-transform duration-200"
      style="width: 1.125rem; height: 1.125rem"
      :class="modelValue ? 'translate-x-[1.4rem]' : 'translate-x-[0.2rem]'"
    />
  </button>
</template>
