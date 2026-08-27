<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { Headphones } from 'lucide-vue-next'
import type { ShopContact } from '@/api/types'
import { useContacts } from '@/composables/useContacts'

/**
 * 「联系客服」按钮 + 弹出菜单。
 *
 * placement 默认向上：它主要用在页脚，那里已经贴着页面底部，
 * 往下弹会直接掉出可视区域。页头用 down。
 */
withDefaults(defineProps<{ placement?: 'up' | 'down' }>(), { placement: 'up' })

const { contacts, iconOf, actionIconOf, pick } = useContacts()
const open = ref(false)
const root = ref<HTMLElement | null>(null)

async function choose(c: ShopContact) {
  await pick(c)
  open.value = false
}

function onDocPointerDown(e: PointerEvent) {
  if (!root.value?.contains(e.target as Node)) open.value = false
}
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') open.value = false
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocPointerDown, true)
  document.addEventListener('keydown', onKey)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocPointerDown, true)
  document.removeEventListener('keydown', onKey)
})
</script>

<template>
  <div v-if="contacts.length" ref="root" class="relative inline-block">
    <Transition :name="placement === 'up' ? 'wd-pop-up' : 'wd-pop'">
      <div
        v-if="open"
        class="absolute right-0 z-40 min-w-56 p-1.5 bg-white border border-gray-100 rounded-xl shadow-2xl shadow-black/10"
        :class="placement === 'up' ? 'bottom-full mb-2' : 'top-full mt-2'"
        role="menu"
      >
        <button
          v-for="(c, i) in contacts"
          :key="`${c.type}-${i}`"
          type="button"
          role="menuitem"
          class="w-full flex items-center gap-2.5 px-3 py-2.5 rounded-lg text-left text-gray-600 hover:bg-[#faf8f5] hover:text-gray-800 transition-colors duration-150"
          @click="choose(c)"
        >
          <component :is="iconOf(c)" class="w-4 h-4 shrink-0 text-[#4a9d9a]" />
          <span class="min-w-0 flex-1">
            <span class="block text-sm">{{ c.label }}</span>
            <span class="block text-xs text-gray-400 truncate">{{ c.value }}</span>
          </span>
          <component :is="actionIconOf(c)" class="w-3.5 h-3.5 shrink-0 text-gray-300" />
        </button>
      </div>
    </Transition>

    <button
      type="button"
      class="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-xl bg-[#4a9d9a] text-white text-xs font-medium shadow-md shadow-[#4a9d9a]/20 hover:shadow-lg hover:-translate-y-0.5 transition-all duration-200"
      :aria-expanded="open"
      aria-haspopup="menu"
      @click="open = !open"
    >
      <Headphones class="w-3.5 h-3.5" />
      联系客服
    </button>
  </div>
</template>
