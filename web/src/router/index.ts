import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useShopStore } from '@/stores/shop'

/**
 * 后台入口路径。
 *
 * 值来自 index.html 里那个 <meta>，由服务端在返回页面时按当前配置填写：
 *   访问的就是后台入口 -> 真实路径
 *   访问其它任何页面   -> 空串
 *
 * 空串时下面根本不注册后台路由，于是 /admin 之类的猜测一律落到 404 页 ——
 * 这是「改了入口就该找不到」的关键：如果照常注册，扫描器随便抓个页面
 * 就能从 JS 里看出后台在哪，改路径就成了摆设。
 *
 * 开发模式（vite 直接吐 index.html，不经过服务端）里它保持出厂的 admin。
 */
const ADMIN_PATH = (
  document.querySelector<HTMLMetaElement>('meta[name="moecard-admin-path"]')?.content || ''
).trim()

/** 后台的子路由。路径都是相对的，挂到哪个前缀下由 ADMIN_PATH 决定 */
const adminChildren: RouteRecordRaw[] = [
  { path: '', redirect: { name: 'admin-dashboard' } },
  {
    path: 'dashboard',
    name: 'admin-dashboard',
    component: () => import('@/views/admin/DashboardView.vue'),
    meta: { title: '控制台', subtitle: '今天的经营概况' },
  },
  {
    path: 'categories',
    name: 'admin-categories',
    component: () => import('@/views/admin/CategoriesView.vue'),
    meta: { title: '商品分类', subtitle: '组织商品的归类方式' },
  },
  {
    path: 'products',
    name: 'admin-products',
    component: () => import('@/views/admin/ProductsView.vue'),
    meta: { title: '商品管理', subtitle: '上架、定价与库存' },
  },
  {
    path: 'codes',
    name: 'admin-codes',
    component: () => import('@/views/admin/CodesView.vue'),
    meta: { title: '卡密管理', subtitle: '全部商品的卡密库存' },
  },
  // 旧的按商品分页路径保留成跳转：卡密只有一个页面，商品是它的筛选条件。
  // 收藏夹和历史链接照样能用，落到同一屏、同一个商品上。
  {
    path: 'products/:id/codes',
    redirect: (to) => ({ name: 'admin-codes', query: { product_id: String(to.params.id) } }),
  },
  {
    path: 'orders',
    name: 'admin-orders',
    component: () => import('@/views/admin/OrdersView.vue'),
    meta: { title: '订单管理', subtitle: '查询、发货与退款' },
  },
  {
    path: 'coupons',
    name: 'admin-coupons',
    component: () => import('@/views/admin/CouponsView.vue'),
    meta: { title: '优惠券', subtitle: '满减与折扣活动' },
  },
  {
    path: 'payments',
    name: 'admin-payments',
    component: () => import('@/views/admin/PaymentsView.vue'),
    meta: { title: '支付方式', subtitle: '收款渠道与回调配置' },
  },
  {
    path: 'settings',
    name: 'admin-settings',
    component: () => import('@/views/admin/SettingsView.vue'),
    meta: { title: '商城设置', subtitle: '站点信息与交易规则' },
  },
  {
    path: 'mail',
    name: 'admin-mail',
    component: () => import('@/views/admin/MailView.vue'),
    meta: { title: '邮件配置', subtitle: 'SMTP 与通知模板' },
  },
  {
    path: 'notify',
    name: 'admin-notify',
    component: () => import('@/views/admin/NotifyView.vue'),
    meta: { title: '商家通知', subtitle: '订单与库存的即时提醒' },
  },
  {
    path: 'admins',
    name: 'admin-admins',
    component: () => import('@/views/admin/AdminsView.vue'),
    meta: { title: '管理员', subtitle: '后台账号与权限' },
  },
  {
    path: 'logs',
    name: 'admin-logs',
    component: () => import('@/views/admin/LogsView.vue'),
    meta: { title: '系统日志', subtitle: '操作、支付与邮件记录' },
  },
  {
    path: 'about',
    name: 'admin-about',
    component: () => import('@/views/admin/AboutView.vue'),
    meta: { title: '关于', subtitle: '项目信息与交流方式' },
  },
]

const adminRoutes: RouteRecordRaw[] = ADMIN_PATH
  ? [
      {
        path: `/${ADMIN_PATH}/login`,
        name: 'admin-login',
        component: () => import('@/views/admin/LoginView.vue'),
        meta: { plain: true },
      },
      {
        path: `/${ADMIN_PATH}`,
        component: () => import('@/layouts/AdminLayout.vue'),
        meta: { requiresAuth: true },
        children: adminChildren,
      },
    ]
  : []

/**
 * 路由设计：
 *   /              商城前台
 *   /<后台入口>/*   管理后台（路径可在后台自定义，默认 admin）
 *
 * 全部按需加载，前台不会下载任何后台代码。
 */
const routes: RouteRecordRaw[] = [
  {
    path: '/',
    component: () => import('@/layouts/ShopLayout.vue'),
    children: [
      { path: '', name: 'home', component: () => import('@/views/shop/HomeView.vue') },
      {
        path: 'category/:slug',
        name: 'category',
        component: () => import('@/views/shop/CategoryView.vue'),
        meta: { title: '商品分类' },
      },
      {
        path: 'product/:slug',
        name: 'product',
        component: () => import('@/views/shop/ProductView.vue'),
      },
      {
        path: 'checkout/:slug',
        name: 'checkout',
        component: () => import('@/views/shop/CheckoutView.vue'),
        meta: { title: '确认订单' },
      },
      {
        path: 'pay/:orderNo',
        name: 'pay',
        component: () => import('@/views/shop/PayView.vue'),
        meta: { title: '收银台' },
      },
      {
        path: 'pay/result',
        name: 'pay-result',
        component: () => import('@/views/shop/PayResultView.vue'),
        meta: { title: '支付结果' },
      },
      {
        path: 'order',
        name: 'order-query',
        component: () => import('@/views/shop/OrderQueryView.vue'),
        meta: { title: '订单查询' },
      },
      {
        path: 'order/:orderNo',
        name: 'order-detail',
        component: () => import('@/views/shop/OrderDetailView.vue'),
        meta: { title: '订单详情' },
      },
    ],
  },

  {
    path: '/setup',
    name: 'setup',
    component: () => import('@/views/SetupView.vue'),
    meta: { plain: true },
  },

  ...adminRoutes,

  {
    path: '/:pathMatch(.*)*',
    name: 'not-found',
    component: () => import('@/views/NotFoundView.vue'),
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior(_to, _from, saved) {
    return saved ?? { top: 0 }
  },
})

router.beforeEach(async (to) => {
  const shop = useShopStore()

  // 商城配置几乎每个页面都要用，在这里统一加载一次。
  // 失败不阻断路由 —— 让页面用默认配置渲染，总比白屏好。
  if (!shop.loaded) {
    await shop.load().catch(() => undefined)
  }

  // 未初始化时强制进入 /setup
  if (!shop.config.installed && to.name !== 'setup') {
    return { name: 'setup' }
  }
  if (shop.config.installed && to.name === 'setup') {
    return { name: 'home' }
  }

  if (to.meta.requiresAuth) {
    const auth = useAuthStore()
    if (!auth.isLoggedIn) {
      return { name: 'admin-login', query: { redirect: to.fullPath } }
    }
    // token 存在但还没拉过用户信息时校验一次，避免拿着失效 token 进后台
    if (!auth.admin) {
      const ok = await auth.fetchProfile()
      if (!ok) return { name: 'admin-login', query: { redirect: to.fullPath } }
    }
  }

  return true
})

router.afterEach((to) => {
  const shop = useShopStore()
  const title = to.meta.title as string | undefined
  if (to.path.startsWith('/admin')) {
    document.title = shop.adminTitle(title)
  } else {
    shop.applyDocumentMeta(title)
  }
})

export default router
