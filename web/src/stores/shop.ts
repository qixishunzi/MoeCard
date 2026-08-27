import { defineStore } from 'pinia'
import { ref } from 'vue'
import { shopApi } from '@/api'
import type { Category, ShopConfig } from '@/api/types'

/** 内置图标。清空站点 Logo 时回到它 */
const DEFAULT_FAVICON = '/favicon-32.png'

const DEFAULT_CONFIG: ShopConfig = {
  site_name: 'MoeCard',
  site_title: 'MoeCard',
  site_description: '',
  site_keywords: '',
  site_logo: '',
  site_notice: '',
  site_footer: '',
  contacts: [],
  banners: [],
  notice_popup: false,
  notice_force_read: false,
  notice_force_seconds: 5,
  icp: '',
  currency: 'CNY',
  currency_symbol: '¥',
  timezone: 'Asia/Shanghai',
  allow_order: true,
  show_sales: true,
  maintenance: false,
  maintenance_text: '',
  order_expire_minutes: 15,
  installed: true,
  // 默认全关：配置还没拉回来之前不该先渲染出一个验证控件
  turnstile: {
    enabled: false,
    site_key: '',
    size: 'normal',
    on_admin_login: false,
    on_order: false,
    on_order_query: false,
    on_coupon: false,
  },
}

/** 商城全局配置。首屏就需要，因此在路由守卫里提前加载。 */
export const useShopStore = defineStore('shop', () => {
  const config = ref<ShopConfig>({ ...DEFAULT_CONFIG })
  const categories = ref<Category[]>([])
  const loaded = ref(false)
  const loading = ref(false)

  async function load(force = false) {
    if (loaded.value && !force) return
    if (loading.value) return
    loading.value = true
    try {
      const [cfg, cats] = await Promise.all([
        shopApi.config(),
        shopApi.categories().catch(() => [] as Category[]),
      ])
      config.value = { ...DEFAULT_CONFIG, ...cfg }
      categories.value = cats
      loaded.value = true
      applyDocumentMeta()
    } finally {
      loading.value = false
    }
  }

  /**
   * 把商城配置同步到 <title> 与 meta，兼顾 SEO 与浏览器标签展示。
   *
   * 后台页面走另一套标题格式。这里必须判一下当前在哪 ——
   * 在「商城设置」里保存后会 load(true) 刷新配置，顺手调到这里，
   * 结果把标签页标题从「商城设置 - 萌卡商城 后台」改成了前台的店名。
   */
  function applyDocumentMeta(pageTitle?: string) {
    const c = config.value
    if (location.pathname.startsWith('/admin')) {
      // 后台标题由路由钩子按 route.meta.title 设置，这里拿不到那个信息。
      // 只在明确传了页面名时才改，否则原样保留 —— 宁可不动，也不要把
      //「商城设置 - 萌卡商城 后台」覆盖成缺了页面名的半截标题。
      if (pageTitle) document.title = adminTitle(pageTitle)
    } else {
      document.title = pageTitle ? `${pageTitle} - ${c.site_name}` : c.site_title || c.site_name
    }

    setMeta('description', c.site_description)
    setMeta('keywords', c.site_keywords)
    applyFavicon(c.site_logo)
  }

  /**
   * 把站点 Logo 同时用作浏览器标签图标。
   *
   * 之前只在页头 <img> 上用了 site_logo，标签页仍然是内置那个 "M" ——
   * 店主换了 Logo 却发现收藏夹里还是别人家的标记。
   *
   * 注意要连 type 一起改掉：原来的 link 上写着 image/svg+xml，
   * 只换 href 指向一张 PNG 的话，部分浏览器会因为类型不符而不加载。
   */
  function applyFavicon(url: string) {
    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    if (!link) return
    if (!url) {
      // 没设 Logo 就回到内置图标（可能是刚刚被清空的）
      if (link.getAttribute('href') !== DEFAULT_FAVICON) {
        link.setAttribute('href', DEFAULT_FAVICON)
        link.setAttribute('type', 'image/svg+xml')
      }
      return
    }
    if (link.getAttribute('href') === url) return
    link.setAttribute('href', url)
    const ext = url.split('?')[0].split('.').pop()?.toLowerCase()
    const types: Record<string, string> = {
      svg: 'image/svg+xml', png: 'image/png', ico: 'image/x-icon',
      jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif', webp: 'image/webp',
    }
    // 认不出扩展名就把 type 去掉，让浏览器自己嗅探，总比写错类型强
    const t = ext ? types[ext] : undefined
    if (t) link.setAttribute('type', t)
    else link.removeAttribute('type')
  }

  /** 后台标题格式，供路由钩子与配置刷新共用，避免两处写法走散。 */
  function adminTitle(pageTitle?: string) {
    const name = config.value.site_name
    return pageTitle ? `${pageTitle} - ${name} 后台` : `${name} 后台`
  }

  function setMeta(name: string, content: string) {
    if (!content) return
    let el = document.querySelector<HTMLMetaElement>(`meta[name="${name}"]`)
    if (!el) {
      el = document.createElement('meta')
      el.name = name
      document.head.appendChild(el)
    }
    el.content = content
  }

  function symbol() {
    return config.value.currency_symbol || '¥'
  }

  return { config, categories, loaded, loading, load, applyDocumentMeta, adminTitle, symbol }
})
