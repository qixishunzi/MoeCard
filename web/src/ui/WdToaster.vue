<script setup lang="ts">
import { CheckCircle2, AlertCircle, Info, TriangleAlert, X } from 'lucide-vue-next'
import { confirmState, dismissToast, resolveConfirm, toastState, type ToastType } from './toast'

const icons: Record<ToastType, typeof CheckCircle2> = {
  success: CheckCircle2,
  error: AlertCircle,
  warning: TriangleAlert,
  info: Info,
}

const tone: Record<ToastType, { bg: string; fg: string }> = {
  success: { bg: 'bg-[#4a9d9a]/10', fg: 'text-[#4a9d9a]' },
  error: { bg: 'bg-[#c17767]/10', fg: 'text-[#c17767]' },
  warning: { bg: 'bg-[#e8b86d]/15', fg: 'text-[#c17767]' },
  info: { bg: 'bg-[#6b8e8e]/10', fg: 'text-[#6b8e8e]' },
}
</script>

<template>
  <!-- 消息条 -->
  <Teleport to="body">
    <div
      class="fixed top-4 left-1/2 -translate-x-1/2 z-[100] flex flex-col items-center gap-2 w-full max-w-md px-4 pointer-events-none"
      role="status"
      aria-live="polite"
    >
      <TransitionGroup
        enter-active-class="transition-all duration-300 ease-out"
        enter-from-class="opacity-0 -translate-y-2"
        leave-active-class="transition-all duration-200 ease-in"
        leave-to-class="opacity-0 -translate-y-1"
        move-class="transition-transform duration-200"
      >
        <div
          v-for="t in toastState.items"
          :key="t.id"
          class="pointer-events-auto w-full flex items-start gap-3 bg-white rounded-2xl shadow-xl shadow-black/[0.06] border border-gray-100 px-4 py-3"
        >
          <span
            class="w-8 h-8 rounded-xl flex items-center justify-center shrink-0"
            :class="tone[t.type].bg"
          >
            <component :is="icons[t.type]" class="w-4 h-4" :class="tone[t.type].fg" />
          </span>
          <p class="flex-1 text-sm text-gray-700 leading-relaxed pt-1 break-words">{{ t.message }}</p>
          <button
            class="p-1 rounded-lg text-gray-300 hover:text-gray-500 hover:bg-gray-50 transition-colors duration-200 shrink-0"
            aria-label="关闭"
            @click="dismissToast(t.id)"
          >
            <X class="w-3.5 h-3.5" />
          </button>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>

  <!-- 确认框 -->
  <Teleport to="body">
    <Transition
      enter-active-class="transition-opacity duration-200"
      enter-from-class="opacity-0"
      leave-active-class="transition-opacity duration-150"
      leave-to-class="opacity-0"
    >
      <div
        v-if="confirmState.open"
        class="fixed inset-0 z-[110] flex items-center justify-center p-4 bg-gray-900/20 backdrop-blur-[2px]"
        role="dialog"
        aria-modal="true"
        @click.self="resolveConfirm(false)"
        @keydown.esc="resolveConfirm(false)"
      >
        <div class="w-full max-w-sm bg-white rounded-2xl shadow-xl shadow-black/[0.08] p-6">
          <h3 class="text-base font-semibold text-gray-800">{{ confirmState.title }}</h3>
          <p class="mt-2 text-sm text-gray-500 leading-relaxed whitespace-pre-line">
            {{ confirmState.message }}
          </p>
          <div class="mt-6 flex justify-end gap-2.5">
            <button
              class="px-4 py-2 text-sm font-medium text-gray-500 rounded-xl border border-gray-200 hover:bg-gray-50 transition-all duration-200"
              @click="resolveConfirm(false)"
            >
              {{ confirmState.cancelText }}
            </button>
            <button
              class="px-4 py-2 text-sm font-medium text-white rounded-xl transition-all duration-200 hover:-translate-y-0.5"
              :class="
                confirmState.tone === 'danger'
                  ? 'bg-[#c17767] shadow-lg shadow-[#c17767]/25 hover:shadow-xl'
                  : 'bg-[#4a9d9a] shadow-lg shadow-[#4a9d9a]/25 hover:shadow-xl'
              "
              autofocus
              @click="resolveConfirm(true)"
            >
              {{ confirmState.confirmText }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
