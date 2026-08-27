<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ChevronLeft, ChevronRight } from 'lucide-vue-next'
import type { ShopBanner } from '@/api/types'
import { RouterLink } from 'vue-router'
import { useShopStore } from '@/stores/shop'

/**
 * 首页顶部轮播图。
 *
 * 只有一张时不做成轮播 —— 没有第二张可切，箭头和圆点只是噪音。
 * 绑定了商品的图整张可点；没绑的就是一张图，不给假的点击反馈。
 */
const shop = useShopStore()
const banners = computed<ShopBanner[]>(() => shop.config.banners ?? [])
const many = computed(() => banners.value.length > 1)

const AUTOPLAY_MS = 3500
const SLIDE_MS = 380
/** 拖动超过这个距离才算翻页，否则弹回原位 */
const DRAG_RATIO = 0.2
const DRAG_MAX = 70

/**
 * 轨道两端各多铺一张：开头放最后一张的复制，末尾放第一张的复制。
 *
 * 这样从最后一张往后翻时，"下一张"已经躺在轨道上，滑过去之后再无动画地
 * 跳回真身。不这么做的话，从末尾回到开头会让整条轨道当着用户的面倒卷一遍。
 */
const track = computed<ShopBanner[]>(() => {
  const b = banners.value
  if (b.length < 2) return b
  return [b[b.length - 1], ...b, b[0]]
})

/** 轨道下标。有复制片时真正的第一张在 1 号位 */
const pos = ref(1)
const animate = ref(true)
const rootEl = ref<HTMLElement>()
const trackEl = ref<HTMLElement>()

/** 对外的"第几张"，圆点和无障碍标注用的是这个，不是轨道下标 */
const index = computed(() => {
  const n = banners.value.length
  if (n < 2) return 0
  return (((pos.value - 1) % n) + n) % n
})

/**
 * 用户明确表示不想要动效时，既不自动播放也不做滑动动画。
 *
 * 自动轮播对前庭功能敏感的人是真的会难受的，而且这个开关是系统级的，
 * 用户设了就说明他是认真的。
 */
const reduced = ref(false)
const duration = computed(() => (reduced.value ? 0 : SLIDE_MS))

let timer: ReturnType<typeof setInterval> | undefined
let normalizeTimer: ReturnType<typeof setTimeout> | undefined

function reset(p: number) {
  pos.value = p
  animate.value = true
}

/**
 * 停在复制片上时，把位置换成对应的真身。
 *
 * 换的过程必须没有过渡，否则用户会看见画面从末尾横扫回开头。
 */
async function normalize() {
  if (!many.value) return
  const n = banners.value.length
  const target = pos.value === n + 1 ? 1 : pos.value === 0 ? n : null
  if (target === null) return

  animate.value = false
  pos.value = target
  await nextTick()
  // 强制重排，让"无过渡 + 新位置"这一帧真正落地再把过渡打开。
  // 不用 requestAnimationFrame：页面在后台标签里时它根本不跑。
  void trackEl.value?.offsetWidth
  animate.value = true
}

/** 过渡结束事件不一定来（时长为 0 时就不会），所以再挂一道定时兜底 */
function scheduleNormalize() {
  clearTimeout(normalizeTimer)
  normalizeTimer = setTimeout(normalize, duration.value + 40)
}

function go(delta: number) {
  if (!many.value) return
  pos.value += delta
  scheduleNormalize()
}

function next() {
  go(1)
}
function prev() {
  go(-1)
}

/** 圆点是直接跳，不经过中间那些张 */
function goTo(i: number) {
  pos.value = many.value ? i + 1 : 0
}

function start() {
  stop()
  if (!many.value || reduced.value) return
  timer = setInterval(next, AUTOPLAY_MS)
}
function stop() {
  if (timer) clearInterval(timer)
  timer = undefined
}

/** 手动切换后重新计时，别让用户刚点完就被自动切走 */
function manual(fn: () => void) {
  fn()
  start()
}

// ---------------------------------------------------------------------------
// 拖动。鼠标和手指走的是同一套 pointer 事件，桌面端也能拖着看下一张
// ---------------------------------------------------------------------------
const dragging = ref(false)
const dragX = ref(0)
let startX = 0
let startY = 0
let axis: 'x' | 'y' | null = null
/** 拖完之后紧跟着的那次 click 要吃掉，否则松手就跳进了商品页 */
let swallowClick = false

function widthOf() {
  return rootEl.value?.clientWidth || 1
}

function onPointerDown(e: PointerEvent) {
  swallowClick = false
  if (!many.value) return
  // 箭头和圆点自己会处理点击，别在它们身上开始拖动 ——
  // 指针捕获会把 click 的目标一起抢走，那两个按钮就按不动了
  if ((e.target as HTMLElement).closest('button')) return
  if (e.pointerType === 'mouse' && e.button !== 0) return

  startX = e.clientX
  startY = e.clientY
  axis = null
  dragging.value = true
  dragX.value = 0
  stop()
}

function onPointerMove(e: PointerEvent) {
  if (!dragging.value) return
  const dx = e.clientX - startX
  const dy = e.clientY - startY

  // 先看清用户是想横着翻页还是竖着滚页面，别把滚动抢走
  if (!axis) {
    if (Math.abs(dx) < 6 && Math.abs(dy) < 6) return
    axis = Math.abs(dx) > Math.abs(dy) ? 'x' : 'y'
    if (axis === 'y') {
      dragging.value = false
      dragX.value = 0
      start()
      return
    }
    rootEl.value?.setPointerCapture?.(e.pointerId)
  }
  dragX.value = dx
}

function onPointerUp() {
  if (!dragging.value) return
  const dx = dragX.value
  dragging.value = false
  dragX.value = 0

  if (Math.abs(dx) > 6) swallowClick = true
  const threshold = Math.min(DRAG_MAX, widthOf() * DRAG_RATIO)
  if (Math.abs(dx) > threshold) go(dx < 0 ? 1 : -1)
  start()
}

function onClickCapture(e: MouseEvent) {
  if (!swallowClick) return
  swallowClick = false
  e.preventDefault()
  e.stopPropagation()
}

// ---------------------------------------------------------------------------

/** 轨道每格宽度都等于容器，所以位移直接按 100% 一格算 */
const trackStyle = computed(() => ({
  transform: `translate3d(calc(${-pos.value * 100}% + ${dragX.value}px), 0, 0)`,
  transitionDuration: animate.value && !dragging.value ? `${duration.value}ms` : '0ms',
}))

function hrefOf(b: ShopBanner) {
  return b.product_slug ? `/product/${b.product_slug}` : undefined
}

/**
 * 绑定了商品的用 RouterLink，没绑的用普通 div。
 *
 * 不用裸 <a href>：那会整页重载，前台是 SPA，重载一次要把所有资源
 * 和商城配置重新拉一遍。RouterLink 渲染出来仍然是真的 <a>，
 * 右键新标签打开、中键点击这些都照常。
 */
function tagOf(b: ShopBanner) {
  return b.product_slug ? RouterLink : 'div'
}

function onTransitionEnd(e: TransitionEvent) {
  if (e.target !== trackEl.value || e.propertyName !== 'transform') return
  normalize()
}

let mq: MediaQueryList | undefined
function onMotionChange() {
  reduced.value = !!mq?.matches
  start()
}

onMounted(() => {
  mq = window.matchMedia?.('(prefers-reduced-motion: reduce)')
  reduced.value = !!mq?.matches
  mq?.addEventListener?.('change', onMotionChange)
  reset(many.value ? 1 : 0)
  start()
})

onBeforeUnmount(() => {
  stop()
  clearTimeout(normalizeTimer)
  mq?.removeEventListener?.('change', onMotionChange)
})

// 后台改完配置刷新过来时，张数可能已经变了，下标得跟着回到起点
watch(
  () => banners.value.length,
  () => {
    reset(many.value ? 1 : 0)
    start()
  },
)
</script>

<template>
  <section
    v-if="banners.length"
    ref="rootEl"
    class="group relative overflow-hidden rounded-2xl bg-[#f0efec] shadow-xl shadow-black/[0.06] select-none touch-pan-y"
    aria-roledescription="carousel"
    aria-label="首页轮播图"
    @mouseenter="stop"
    @mouseleave="start"
    @pointerdown="onPointerDown"
    @pointermove="onPointerMove"
    @pointerup="onPointerUp"
    @pointercancel="onPointerUp"
    @click.capture="onClickCapture"
  >
    <!--
      固定宽高比 + object-cover：图片尺寸参差不齐时，
      不定高会让首页在每次切换时上下跳一下。
    -->
    <div class="relative w-full aspect-[16/9] sm:aspect-[3/1]">
      <div
        ref="trackEl"
        class="flex h-full w-full ease-out"
        :class="dragging ? '' : 'transition-transform'"
        :style="trackStyle"
        @transitionend="onTransitionEnd"
      >
        <component
          :is="tagOf(b)"
          v-for="(b, i) in track"
          :key="`${i}-${b.image}`"
          :to="hrefOf(b)"
          class="relative w-full h-full shrink-0"
          :class="hrefOf(b) && 'cursor-pointer'"
          :aria-hidden="i === pos ? undefined : 'true'"
          :tabindex="i === pos && hrefOf(b) ? undefined : -1"
        >
          <img
            :src="b.image"
            :alt="b.title || b.product_name || `轮播图 ${index + 1}`"
            class="w-full h-full object-cover"
            draggable="false"
            :loading="i <= 1 ? 'eager' : 'lazy'"
          />
          <!-- 有说明文字时压一层渐变，否则浅色图上的白字看不清 -->
          <div
            v-if="b.title"
            class="absolute inset-x-0 bottom-0 px-5 sm:px-8 py-4 sm:py-6 bg-gradient-to-t from-black/55 to-transparent"
          >
            <p class="text-sm sm:text-lg font-medium text-white drop-shadow">{{ b.title }}</p>
          </div>
        </component>
      </div>
    </div>

    <template v-if="many">
      <!--
        平时几乎看不见，鼠标进到图上才浮出来，指到按钮上再变实。
        图本身才是主角，两个常驻的白圆片会一直和它抢注意力。
      -->
      <button
        type="button"
        class="hidden sm:grid absolute left-3 top-1/2 -translate-y-1/2 w-9 h-9 place-items-center rounded-full bg-black/10 text-white/50 backdrop-blur-[2px] opacity-0 group-hover:opacity-100 group-hover:bg-black/25 group-hover:text-white/85 hover:!bg-white/90 hover:!text-gray-800 hover:shadow-md focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-white transition-all duration-200"
        aria-label="上一张"
        @click="manual(prev)"
      >
        <ChevronLeft class="w-4 h-4" />
      </button>
      <button
        type="button"
        class="hidden sm:grid absolute right-3 top-1/2 -translate-y-1/2 w-9 h-9 place-items-center rounded-full bg-black/10 text-white/50 backdrop-blur-[2px] opacity-0 group-hover:opacity-100 group-hover:bg-black/25 group-hover:text-white/85 hover:!bg-white/90 hover:!text-gray-800 hover:shadow-md focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-white transition-all duration-200"
        aria-label="下一张"
        @click="manual(next)"
      >
        <ChevronRight class="w-4 h-4" />
      </button>

      <div class="absolute inset-x-0 bottom-3 flex items-center justify-center gap-2">
        <button
          v-for="(_, i) in banners"
          :key="`dot-${i}`"
          type="button"
          class="h-1.5 rounded-full transition-all duration-200"
          :class="i === index ? 'w-6 bg-white' : 'w-1.5 bg-white/60 hover:bg-white/90'"
          :aria-label="`切换到第 ${i + 1} 张`"
          :aria-current="i === index ? 'true' : undefined"
          @click="manual(() => goTo(i))"
        />
      </div>
    </template>
  </section>
</template>
