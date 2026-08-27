<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Check, ChevronDown } from 'lucide-vue-next'

/**
 * 暖色仪表盘下拉选择。
 *
 * 为什么不用原生 <select>：把 <select> 的按钮部分改成什么样都行，
 * 但展开后那张选项面板是操作系统画的 —— Windows 上是一块灰白列表，
 * 圆角、配色、间距全都和后台其它控件对不上，而且没法改。
 * 所以这里自己实现一个 listbox：按钮 + 浮层，两部分都归 CSS 管。
 *
 * 自己实现就得自己补齐原生控件白送的东西，缺一样就是可用性倒退：
 *   - 键盘：↑↓ 移动、Enter/空格 选中、Esc 关闭、Home/End 跳首尾、字母跳转
 *   - 屏幕阅读器：combobox / listbox / option 角色与 aria-activedescendant
 *   - 点击外部关闭、滚动时跟随、失焦收起
 *
 * 浮层用 Teleport 挂到 body 并按视口坐标定位：
 * 下拉常常出现在带 overflow 的工具栏或表格里，留在原地会被裁掉半截。
 * z-index 取 95 —— 要压过弹窗遮罩(90)，又要低于 toast(100) 和二次确认(110)。
 */
export type SelectOption = { label: string; value: string | number; disabled?: boolean }

const props = withDefaults(
  defineProps<{
    modelValue?: string | number | null
    options?: SelectOption[]
    placeholder?: string
    /** 允许选“全部”（值为空字符串）*/
    clearable?: boolean
    disabled?: boolean
    error?: boolean
    /** 触发按钮的无障碍名称，没有外部 label 时必须给 */
    ariaLabel?: string
    /** 关联的 label 元素 id */
    labelledby?: string
    /** sm 用于分页这类紧凑位置 */
    size?: 'sm' | 'md'
  }>(),
  { options: () => [], size: 'md' },
)

const emit = defineEmits<{
  'update:modelValue': [value: string | number | null]
  change: []
}>()

const open = ref(false)
const activeIndex = ref(-1)
const triggerEl = ref<HTMLButtonElement | null>(null)
const panelEl = ref<HTMLElement | null>(null)
const panelStyle = ref<Record<string, string>>({})
const uid = Math.random().toString(36).slice(2, 8)

/** 空值选项排在最前，和原生 select 的 placeholder option 行为一致 */
const items = computed<SelectOption[]>(() => {
  const rest = props.options ?? []
  if (props.placeholder || props.clearable) {
    return [{ label: props.placeholder || '全部', value: '' }, ...rest]
  }
  return rest
})

const selectedIndex = computed(() =>
  items.value.findIndex((o) => String(o.value) === String(props.modelValue ?? '')),
)

const display = computed(() => {
  const v = String(props.modelValue ?? '')
  if (v === '') return props.placeholder || items.value[0]?.label || '请选择'
  const hit = items.value.find((o) => String(o.value) === v)
  // 选项还没异步加载出来时显示裸 ID，而不是回落到 placeholder ——
  // 明明按商品 3 过滤着，按钮上却写"全部商品"，比显示 #3 误导得多
  return hit ? hit.label : `#${v}`
})

const isPlaceholder = computed(() => String(props.modelValue ?? '') === '')

/**
 * 计算浮层位置。
 *
 * 默认贴在按钮下方；下方装不下就翻到上方 —— 表格最后一行的下拉
 * 如果硬往下开，选项会全部落在视口外面。
 */
function place() {
  const el = triggerEl.value
  if (!el) return
  const r = el.getBoundingClientRect()
  const gap = 6
  const maxH = 288 // 与面板的 max-height 保持一致
  const below = window.innerHeight - r.bottom - gap
  const above = r.top - gap
  const dropUp = below < Math.min(maxH, 160) && above > below

  panelStyle.value = {
    position: 'fixed',
    left: `${Math.round(r.left)}px`,
    width: `${Math.round(r.width)}px`,
    maxHeight: `${Math.round(Math.min(maxH, dropUp ? above : below))}px`,
    ...(dropUp
      ? { bottom: `${Math.round(window.innerHeight - r.top + gap)}px` }
      : { top: `${Math.round(r.bottom + gap)}px` }),
  }
}

async function openPanel() {
  if (props.disabled) return
  open.value = true
  activeIndex.value = selectedIndex.value >= 0 ? selectedIndex.value : 0
  await nextTick()
  place()
  scrollActiveIntoView()
}

function closePanel(refocus = true) {
  if (!open.value) return
  open.value = false
  activeIndex.value = -1
  if (refocus) triggerEl.value?.focus()
}

function toggle() {
  open.value ? closePanel() : openPanel()
}

function pick(i: number) {
  const o = items.value[i]
  if (!o || o.disabled) return
  const v = o.value === '' ? '' : o.value
  if (String(v) !== String(props.modelValue ?? '')) {
    emit('update:modelValue', v)
    emit('change')
  }
  closePanel()
}

/** 跳过 disabled 项移动高亮 */
function move(step: number) {
  const n = items.value.length
  if (!n) return
  let i = activeIndex.value
  for (let k = 0; k < n; k++) {
    i = (i + step + n) % n
    if (!items.value[i].disabled) break
  }
  activeIndex.value = i
  scrollActiveIntoView()
}

function scrollActiveIntoView() {
  nextTick(() => {
    panelEl.value
      ?.querySelector<HTMLElement>(`#opt-${uid}-${activeIndex.value}`)
      ?.scrollIntoView({ block: 'nearest' })
  })
}

/** 连续敲字母跳到对应选项，和原生 select 一样 */
let typed = ''
let typedTimer: ReturnType<typeof setTimeout> | undefined
function typeAhead(ch: string) {
  clearTimeout(typedTimer)
  typed += ch.toLowerCase()
  typedTimer = setTimeout(() => (typed = ''), 700)
  const from = activeIndex.value + (typed.length > 1 ? 0 : 1)
  const n = items.value.length
  for (let k = 0; k < n; k++) {
    const i = (from + k + n) % n
    if (items.value[i].label.toLowerCase().startsWith(typed)) {
      activeIndex.value = i
      scrollActiveIntoView()
      return
    }
  }
}

function onKeydown(e: KeyboardEvent) {
  if (props.disabled) return
  switch (e.key) {
    case 'ArrowDown':
      e.preventDefault()
      open.value ? move(1) : openPanel()
      break
    case 'ArrowUp':
      e.preventDefault()
      open.value ? move(-1) : openPanel()
      break
    case 'Home':
      if (open.value) {
        e.preventDefault()
        activeIndex.value = -1
        move(1)
      }
      break
    case 'End':
      if (open.value) {
        e.preventDefault()
        activeIndex.value = items.value.length
        move(-1)
      }
      break
    case 'Enter':
    case ' ':
      e.preventDefault()
      open.value ? pick(activeIndex.value) : openPanel()
      break
    case 'Escape':
      if (open.value) {
        e.preventDefault()
        closePanel()
      }
      break
    case 'Tab':
      closePanel(false) // 让焦点正常走到下一个控件
      break
    default:
      if (e.key.length === 1 && !e.metaKey && !e.ctrlKey && !e.altKey) {
        if (!open.value) openPanel()
        typeAhead(e.key)
      }
  }
}

function onDocPointerDown(e: PointerEvent) {
  const t = e.target as Node
  if (triggerEl.value?.contains(t) || panelEl.value?.contains(t)) return
  closePanel(false)
}

/**
 * 页面滚动时重新定位。
 *
 * 用 capture 监听：真正滚动的往往是某个内层容器（表格、抽屉），
 * 冒泡阶段收不到它的 scroll 事件，浮层就会僵在原地和按钮分家。
 */
function onScrollOrResize() {
  if (open.value) place()
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocPointerDown, true)
  window.addEventListener('scroll', onScrollOrResize, true)
  window.addEventListener('resize', onScrollOrResize)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocPointerDown, true)
  window.removeEventListener('scroll', onScrollOrResize, true)
  window.removeEventListener('resize', onScrollOrResize)
  clearTimeout(typedTimer)
})

// 选项变了（比如商品列表异步加载完）会改变面板高度，展开着就重新定位一次
watch(
  () => props.options,
  () => {
    if (open.value) nextTick(place)
  },
)
</script>

<template>
  <div class="relative">
    <button
      ref="triggerEl"
      type="button"
      role="combobox"
      :aria-expanded="open"
      :aria-controls="`listbox-${uid}`"
      :aria-activedescendant="open && activeIndex >= 0 ? `opt-${uid}-${activeIndex}` : undefined"
      :aria-label="ariaLabel"
      :aria-labelledby="labelledby"
      :disabled="disabled"
      class="w-full flex items-center gap-2 bg-white border text-left transition-all duration-200 focus:outline-none"
      :class="[
        size === 'sm' ? 'px-2.5 py-1 rounded-lg text-xs' : 'px-3.5 py-2.5 rounded-xl text-sm',
        error
          ? 'border-[#c17767]/60 focus:ring-2 focus:ring-[#c17767]/20 focus:border-[#c17767]'
          : open
            ? 'border-[#4a9d9a] ring-2 ring-[#4a9d9a]/30'
            : 'border-gray-200 hover:border-gray-300 focus:ring-2 focus:ring-[#4a9d9a]/30 focus:border-[#4a9d9a]',
        disabled ? 'bg-gray-50 text-gray-400 cursor-not-allowed' : 'cursor-pointer',
        isPlaceholder && !disabled ? 'text-gray-400' : 'text-gray-800',
      ]"
      @click="toggle"
      @keydown="onKeydown"
    >
      <span class="flex-1 truncate">{{ display }}</span>
      <ChevronDown
        class="shrink-0 text-gray-400 transition-transform duration-200"
        :class="[size === 'sm' ? 'w-3.5 h-3.5' : 'w-4 h-4', open && 'rotate-180']"
      />
    </button>

    <Teleport to="body">
      <Transition name="wd-pop">
        <div
          v-if="open"
          :id="`listbox-${uid}`"
          ref="panelEl"
          role="listbox"
          :aria-label="ariaLabel"
          class="z-[95] overflow-y-auto no-scrollbar p-1.5 bg-white border border-gray-100 rounded-xl shadow-2xl shadow-black/10"
          :style="panelStyle"
        >
          <button
            v-for="(o, i) in items"
            :id="`opt-${uid}-${i}`"
            :key="`${o.value}-${i}`"
            type="button"
            role="option"
            :aria-selected="i === selectedIndex"
            :disabled="o.disabled"
            class="w-full flex items-center gap-2 rounded-lg text-left transition-colors duration-150"
            :class="[
              size === 'sm' ? 'px-2.5 py-1.5 text-xs' : 'px-3 py-2 text-sm',
              o.disabled
                ? 'text-gray-300 cursor-not-allowed'
                : i === selectedIndex
                  ? 'bg-[#4a9d9a] text-white'
                  : i === activeIndex
                    ? 'bg-[#faf8f5] text-gray-800'
                    : 'text-gray-600 hover:bg-[#faf8f5] hover:text-gray-800',
              String(o.value) === '' && 'text-gray-400',
            ]"
            @click="pick(i)"
            @mousemove="activeIndex = i"
          >
            <span class="flex-1 truncate">{{ o.label }}</span>
            <Check v-if="i === selectedIndex" class="w-3.5 h-3.5 shrink-0" />
          </button>

          <p v-if="!items.length" class="px-3 py-6 text-center text-sm text-gray-400">暂无选项</p>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>
