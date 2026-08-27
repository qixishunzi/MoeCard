<script setup lang="ts">
import { computed, ref } from 'vue'
import { Eye, EyeOff } from 'lucide-vue-next'
import WdSelect from './WdSelect.vue'

/**
 * 暖色仪表盘输入控件。
 *
 * 一个组件覆盖 text / password / number / date / textarea / select，
 * 避免为每种类型各写一个几乎相同的文件。
 */
const props = withDefaults(
  defineProps<{
    modelValue?: string | number | null
    type?: 'text' | 'password' | 'number' | 'date' | 'email' | 'search' | 'textarea' | 'select'
    label?: string
    placeholder?: string
    hint?: string
    error?: string
    required?: boolean
    disabled?: boolean
    rows?: number
    min?: number
    max?: number
    step?: number
    maxlength?: number
    /** type=select 时的选项 */
    options?: { label: string; value: string | number }[]
    /** select 是否允许清空 */
    clearable?: boolean
    mono?: boolean
    /** 没有可见 label 时（如日期筛选框）用它提供可访问名 */
    ariaLabel?: string
  }>(),
  { type: 'text', rows: 3 },
)

const emit = defineEmits<{
  'update:modelValue': [value: string | number | null]
  enter: []
  change: []
}>()

const showPassword = ref(false)

/**
 * label 与控件的关联。
 *
 * 光把 <label> 放在输入框上面是不够的：读屏软件会把它念成一个没有名字的
 * 输入框，点标签也不会聚焦到输入框。原生控件用 for/id 配对，
 * 自定义下拉是个 button，只能用 aria-labelledby。
 */
const uid = Math.random().toString(36).slice(2, 8)
const labelId = `wd-label-${uid}`
const controlId = `wd-input-${uid}`
const hintId = `wd-hint-${uid}`

const inputType = computed(() => {
  if (props.type === 'password') return showPassword.value ? 'text' : 'password'
  return props.type
})

const base = computed(() => [
  'w-full bg-white border rounded-xl text-sm text-gray-800 placeholder:text-gray-300',
  'transition-all duration-200 focus:outline-none',
  props.error
    ? 'border-[#c17767]/60 focus:ring-2 focus:ring-[#c17767]/20 focus:border-[#c17767]'
    : 'border-gray-200 focus:ring-2 focus:ring-[#4a9d9a]/30 focus:border-[#4a9d9a]',
  props.disabled && 'bg-gray-50 text-gray-400 cursor-not-allowed',
  props.mono && 'font-mono text-[13px]',
])

function onInput(e: Event) {
  const el = e.target as HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement
  let v: string | number | null = el.value
  if (props.type === 'number') v = el.value === '' ? null : Number(el.value)
  emit('update:modelValue', v)
}
</script>

<template>
  <div>
    <label
      v-if="label"
      :id="labelId"
      :for="type === 'select' ? undefined : controlId"
      class="block mb-1.5 text-xs font-medium text-gray-500"
    >
      {{ label }}
      <span v-if="required" class="text-[#c17767]">*</span>
    </label>

    <div class="relative">
      <textarea
        v-if="type === 'textarea'"
        :id="controlId"
        :value="modelValue as string"
        :aria-describedby="error || hint ? hintId : undefined"
        :aria-invalid="error ? true : undefined"
        :rows="rows"
        :placeholder="placeholder"
        :aria-label="label ? undefined : ariaLabel || placeholder || undefined"
        :disabled="disabled"
        :maxlength="maxlength"
        :class="[...base, 'px-3.5 py-2.5 resize-y leading-relaxed']"
        @input="onInput"
        @change="emit('change')"
      />

      <WdSelect
        v-else-if="type === 'select'"
        :model-value="modelValue"
        :options="options ?? []"
        :placeholder="placeholder"
        :clearable="clearable"
        :disabled="disabled"
        :error="!!error"
        :aria-label="label ? undefined : ariaLabel || placeholder"
        :labelledby="label ? labelId : undefined"
        @update:model-value="(v) => emit('update:modelValue', v)"
        @change="emit('change')"
      />

      <input
        v-else
        :id="controlId"
        :value="modelValue ?? ''"
        :type="inputType"
        :aria-describedby="error || hint ? hintId : undefined"
        :aria-invalid="error ? true : undefined"
        :placeholder="placeholder"
        :aria-label="label ? undefined : ariaLabel || placeholder || undefined"
        :disabled="disabled"
        :min="min"
        :max="max"
        :step="step"
        :maxlength="maxlength"
        :class="[...base, 'px-3.5 py-2.5', type === 'password' && 'pr-10']"
        @input="onInput"
        @change="emit('change')"
        @keyup.enter="emit('enter')"
      />

      <button
        v-if="type === 'password'"
        type="button"
        class="absolute right-2.5 top-1/2 -translate-y-1/2 p-1 rounded-lg text-gray-300 hover:text-gray-500 transition-colors duration-200"
        :aria-label="showPassword ? '隐藏密码' : '显示密码'"
        @click="showPassword = !showPassword"
      >
        <component :is="showPassword ? EyeOff : Eye" class="w-4 h-4" />
      </button>
    </div>

    <p v-if="error" :id="hintId" class="mt-1.5 text-xs text-[#c17767] leading-relaxed">
      {{ error }}
    </p>
    <p v-else-if="hint" :id="hintId" class="mt-1.5 text-xs text-gray-500 leading-relaxed">
      <slot name="hint">{{ hint }}</slot>
    </p>
  </div>
</template>
