/**
 * 暖色仪表盘（Warm Dashboard）UI 套件。
 *
 * 参照 `模板/管理员` 的视觉规范手写，替代 Element Plus ——
 * 硬套组件库的样式做不出模板的效果，而且会一直和它的默认样式打架。
 * 顺带少掉一个 800KB JS + 240KB CSS 的依赖。
 */
export { default as WdBadge } from './WdBadge.vue'
export { default as WdButton } from './WdButton.vue'
export { default as WdCard } from './WdCard.vue'
export { default as WdInput } from './WdInput.vue'
export { default as WdModal } from './WdModal.vue'
export { default as WdPagination } from './WdPagination.vue'
export { default as WdSelect } from './WdSelect.vue'
export { default as WdSwitch } from './WdSwitch.vue'
export { default as WdTable } from './WdTable.vue'
export { default as WdTabs } from './WdTabs.vue'
export { default as WdToaster } from './WdToaster.vue'
export { default as WdTurnstile } from './WdTurnstile.vue'

export type { Column } from './WdTable.vue'
export type { SelectOption } from './WdSelect.vue'
export { toast, confirmDialog } from './toast'
