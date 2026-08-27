<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Megaphone } from 'lucide-vue-next'
import { useShopStore } from '@/stores/shop'
import { WdButton, WdModal } from '@/ui'

/**
 * 公告弹窗。
 *
 * 写在页面里的公告很容易被划过去；真要人看见就得挡在面前一次。
 * 但也只该挡一次 —— 每次进站都弹，用户学会的只是「闭眼点关闭」。
 * 所以按公告内容记一个指纹，内容不变就不再打扰；店主改了公告，
 * 所有人会重新看到一次。
 */
const shop = useShopStore()

const visible = ref(false)
const remaining = ref(0)
let timer: ReturnType<typeof setInterval> | undefined

const text = computed(() => (shop.config.site_notice || '').trim())
const forceRead = computed(() => !!shop.config.notice_force_read)
/** 强制阅读时，倒计时没走完不给关 */
const locked = computed(() => forceRead.value && remaining.value > 0)

/**
 * 用内容指纹做记忆键。
 *
 * 不用「看过就永远不再弹」的单一标记：那样店主改了公告也没人知道。
 * djb2 足够区分文本变化，且不需要引入任何依赖。
 */
function fingerprint(s: string) {
  let h = 5381
  for (let i = 0; i < s.length; i++) h = ((h << 5) + h + s.charCodeAt(i)) | 0
  return `moecard_notice_${(h >>> 0).toString(36)}`
}

function alreadySeen(key: string) {
  try {
    return localStorage.getItem(key) === '1'
  } catch {
    // 隐私模式下读不到就当没看过：宁可多弹一次，也别把公告吞掉
    return false
  }
}

function remember(key: string) {
  try {
    localStorage.setItem(key, '1')
  } catch {
    /* 存不下就算了，下次再弹一遍而已 */
  }
}

function close() {
  if (locked.value) return
  visible.value = false
  remember(fingerprint(text.value))
}

onMounted(() => {
  if (!shop.config.notice_popup || !text.value) return
  if (alreadySeen(fingerprint(text.value))) return

  visible.value = true
  if (!forceRead.value) return

  remaining.value = Math.max(1, Math.min(60, shop.config.notice_force_seconds || 5))
  timer = setInterval(() => {
    remaining.value--
    if (remaining.value <= 0 && timer) clearInterval(timer)
  }, 1000)
})

onBeforeUnmount(() => timer && clearInterval(timer))
</script>

<template>
  <WdModal
    :model-value="visible"
    title="公告"
    width="520px"
    :close-on-overlay="!locked"
    :closable="!locked"
    @update:model-value="close"
  >
    <div class="flex items-start gap-3">
      <span class="w-9 h-9 shrink-0 rounded-xl bg-[#4a9d9a]/10 grid place-items-center">
        <Megaphone class="w-4 h-4 text-[#4a9d9a]" />
      </span>
      <p class="flex-1 min-w-0 text-sm text-gray-700 leading-relaxed whitespace-pre-line">
        {{ text }}
      </p>
    </div>

    <template #footer>
      <WdButton variant="primary" :disabled="locked" @click="close">
        {{ locked ? `请阅读 ${remaining} 秒` : '我知道了' }}
      </WdButton>
    </template>
  </WdModal>
</template>
