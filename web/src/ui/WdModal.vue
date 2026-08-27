<script setup lang="ts">
import { onBeforeUnmount, watch } from 'vue'
import { X } from 'lucide-vue-next'

/**
 * 模态框 / 抽屉。
 *
 * 合成一个组件：`side="right"` 时从右侧滑出（订单详情用），
 * 否则居中弹出。两者的遮罩、锁滚动、Esc 关闭逻辑完全一致，没必要拆成两个文件。
 *
 * 动画只用一个 <Transition>：遮罩淡入淡出，面板的位移由 CSS 后代选择器驱动
 * （见 main.css 的 .wd-modal-* / .wd-drawer-*）。
 * 之前把面板套在第二个 <Transition> 里，Teleport + 嵌套过渡在关闭时会把
 * 节点卡在 leave 阶段：DOM 留在 body 上、事件监听已被解绑，弹窗再也关不掉。
 */
const props = withDefaults(
  defineProps<{
    modelValue: boolean
    title?: string
    subtitle?: string
    width?: string
    side?: 'center' | 'right'
    /** 点遮罩是否关闭。表单类弹窗建议关掉，避免误触丢失填写内容 */
    closeOnOverlay?: boolean
    /**
     * 是否允许用户主动关闭。
     *
     * 为 false 时隐藏右上角的 ×、且 Esc 也不生效 ——
     * 用在「必须读完才能关」这类场景。留着任一条出路，强制就成了摆设。
     */
    closable?: boolean
  }>(),
  { width: '520px', side: 'center', closeOnOverlay: true, closable: true },
)

const emit = defineEmits<{ 'update:modelValue': [v: boolean] }>()

function close() {
  if (!props.closable) return
  emit('update:modelValue', false)
}

/**
 * Esc 监听挂在 document 上。
 * 挂在遮罩 div 的 @keydown.esc 是收不到事件的 —— div 不可聚焦，
 * 焦点还留在触发弹窗的按钮或 body 上，键盘事件根本不会冒泡到它。
 */
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') close()
}

watch(
  () => props.modelValue,
  (open) => {
    // 打开时锁住背景滚动，关闭后恢复
    document.body.style.overflow = open ? 'hidden' : ''
    if (open) document.addEventListener('keydown', onKeydown)
    else document.removeEventListener('keydown', onKeydown)
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  document.body.style.overflow = ''
  document.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <Teleport to="body">
    <Transition :name="side === 'right' ? 'wd-drawer' : 'wd-modal'">
      <div
        v-if="modelValue"
        class="fixed inset-0 z-[90] bg-gray-900/20 backdrop-blur-[2px]"
        :class="side === 'right' ? 'flex justify-end' : 'flex items-center justify-center p-4'"
        role="dialog"
        aria-modal="true"
        @click.self="closeOnOverlay && close()"
      >
        <div
          class="wd-modal-panel bg-white flex flex-col w-full shadow-2xl shadow-black/10"
          :class="side === 'right' ? 'h-full rounded-l-2xl' : 'rounded-2xl max-h-[88vh]'"
          :style="{ maxWidth: width }"
        >
          <header
            class="flex items-start justify-between gap-4 px-6 py-5 border-b border-gray-100 shrink-0"
          >
            <div class="min-w-0">
              <h3 class="text-base font-semibold text-gray-800 truncate">{{ title }}</h3>
              <p v-if="subtitle" class="text-xs text-gray-500 mt-0.5 truncate">{{ subtitle }}</p>
            </div>
            <button
              v-if="closable"
              class="p-1.5 -mr-1.5 rounded-lg text-gray-300 hover:text-gray-600 hover:bg-gray-50 transition-all duration-200 shrink-0"
              aria-label="关闭"
              @click="close"
            >
              <X class="w-4 h-4" />
            </button>
          </header>

          <div class="flex-1 overflow-y-auto px-6 py-5">
            <slot />
          </div>

          <footer
            v-if="$slots.footer"
            class="flex items-center justify-end gap-2.5 px-6 py-4 border-t border-gray-100 shrink-0"
          >
            <slot name="footer" />
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
