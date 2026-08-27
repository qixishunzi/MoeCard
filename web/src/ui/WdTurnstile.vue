<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useShopStore } from '@/stores/shop'

/**
 * Cloudflare Turnstile 控件。
 *
 * 用显式渲染（api.js?render=explicit）而不是 class="cf-turnstile" 隐式扫描：
 * 这是个 SPA，控件所在的页面是路由切换时才挂上来的，
 * 隐式模式只在脚本加载那一刻扫一遍 DOM，之后再进来的页面它看不见。
 *
 * 令牌是一次性的，且 300 秒后过期。所以：
 *   - 提交失败后必须 reset()，否则下一次提交会撞上 timeout-or-duplicate
 *   - 过期回调里要清空令牌，不能让用户拿着一张废票点提交
 */
const props = withDefaults(
  defineProps<{
    /** 当前页面是否需要验证。为 false 时整个组件不渲染，也不加载脚本 */
    scene: 'admin_login' | 'order' | 'order_query' | 'coupon'
    /** 深浅色。跟着页面走，后台和前台都是浅色系 */
    theme?: 'auto' | 'light' | 'dark'
    /**
     * 无视场景开关强制渲染，配合 siteKey 使用。
     * 后台「测试配置」要在功能还没启用时就能试一次密钥 ——
     * 不然只能先开启、再发现填错、然后把自己锁在登录页外面。
     */
    force?: boolean
    /** 强制模式下用这个 sitekey，而不是已保存的配置 */
    siteKey?: string
  }>(),
  { theme: 'light' },
)

const modelValue = defineModel<string>({ default: '' })

const shop = useShopStore()
const container = ref<HTMLElement | null>(null)
const widgetId = ref<string | undefined>()
const failed = ref('')

/** 这个场景要不要验证，由后端下发的配置决定 */
const SCENE_KEY = {
  admin_login: 'on_admin_login',
  order: 'on_order',
  order_query: 'on_order_query',
  coupon: 'on_coupon',
} as const

function activeSiteKey() {
  if (props.force) return (props.siteKey || '').trim()
  return shop.config.turnstile?.site_key || ''
}

function needed() {
  if (props.force) return !!activeSiteKey()
  const t = shop.config.turnstile
  return !!t?.enabled && !!t.site_key && !!t[SCENE_KEY[props.scene]]
}

const SCRIPT_ID = 'cf-turnstile-script'
const SCRIPT_URL = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'

/** 全站只插一次脚本；重复调用共用同一个 Promise */
let loading: Promise<void> | null = null
function loadScript(): Promise<void> {
  if (window.turnstile) return Promise.resolve()
  if (loading) return loading
  loading = new Promise<void>((resolve, reject) => {
    const existing = document.getElementById(SCRIPT_ID) as HTMLScriptElement | null
    if (existing) {
      existing.addEventListener('load', () => resolve())
      existing.addEventListener('error', () => reject(new Error('脚本加载失败')))
      return
    }
    const el = document.createElement('script')
    el.id = SCRIPT_ID
    el.src = SCRIPT_URL
    el.async = true
    el.defer = true
    el.onload = () => resolve()
    el.onerror = () => reject(new Error('脚本加载失败'))
    document.head.appendChild(el)
  })
  return loading
}

async function render() {
  if (!needed() || widgetId.value !== undefined) return
  // needed() 刚从 false 翻成 true 时（配置异步加载完、或后台刚填上 sitekey），
  // v-if 的那个 div 还没被创建出来，必须等一帧再取 ref
  await nextTick()
  if (!needed() || !container.value || widgetId.value !== undefined) return
  try {
    await loadScript()
  } catch {
    // 网络到不了 Cloudflare。这里只提示，不阻断页面渲染 ——
    // 服务端仍会拦截没带令牌的请求，用户至少能看懂为什么提交不了。
    failed.value = '无法加载人机验证组件，请检查网络后刷新页面'
    return
  }
  if (!window.turnstile || !container.value) return

  widgetId.value = window.turnstile.render(container.value, {
    sitekey: activeSiteKey(),
    theme: props.theme,
    size: (shop.config.turnstile?.size as 'normal' | 'flexible' | 'compact') || 'normal',
    callback: (token: string) => {
      failed.value = ''
      modelValue.value = token
    },
    'error-callback': () => {
      modelValue.value = ''
      failed.value = '人机验证出错，请重试'
    },
    'expired-callback': () => {
      // 令牌只有 5 分钟有效期。过期不清空的话，用户填完长表单一提交
      // 会收到一句莫名其妙的"验证已过期"，却看不到控件有任何变化。
      modelValue.value = ''
      failed.value = '人机验证已过期，请重新验证'
      reset()
    },
  })
}

/** 重置控件，拿一张新票。提交失败后必须调用 —— 旧令牌已经用掉了。 */
function reset() {
  modelValue.value = ''
  if (widgetId.value !== undefined && window.turnstile) {
    window.turnstile.reset(widgetId.value)
  }
}

defineExpose({ reset, needed })

onMounted(render)

// 后台改完配置，前台 store 刷新后要能立刻长出控件来
watch(() => shop.config.turnstile, render, { deep: true })

// 后台在输入框里换了 sitekey：把旧控件拆掉重画，否则一直是旧 key 的那个
watch(
  () => props.siteKey,
  () => {
    if (!props.force) return
    if (widgetId.value !== undefined && window.turnstile) {
      window.turnstile.remove(widgetId.value)
      widgetId.value = undefined
    }
    modelValue.value = ''
    render()
  },
)

onBeforeUnmount(() => {
  if (widgetId.value !== undefined && window.turnstile) {
    window.turnstile.remove(widgetId.value)
  }
})
</script>

<template>
  <div v-if="needed()" class="space-y-2">
    <div ref="container" />
    <p v-if="failed" class="text-xs text-[#c17767] leading-relaxed">{{ failed }}</p>
  </div>
</template>
