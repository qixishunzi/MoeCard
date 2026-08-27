<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  AlertTriangle,
  ChevronDown,
  ChevronUp,
  ImagePlus,
  Loader2,
  Plus,
  Save,
  ShieldCheck,
  Trash2,
} from 'lucide-vue-next'
import { ApiError, adminApi } from '@/api'
import type { SettingsRuntime } from '@/api/types'
import { useShopStore } from '@/stores/shop'
import { WdBadge, WdButton, WdCard, WdInput, WdSwitch, WdTabs, WdTurnstile, toast } from '@/ui'

const shop = useShopStore()

const loading = ref(true)
const saving = ref(false)
const uploading = ref(false)
const tab = ref('site')

/** 可以单独开关人机验证的场景 */
const CAPTCHA_SCENES = [
  {
    key: 'turnstile_on_admin_login' as const,
    title: '后台登录',
    desc: '挡住对管理员密码的暴力破解。这是最该开的一个',
    recommended: true,
  },
  {
    key: 'turnstile_on_order' as const,
    title: '商品下单',
    desc: '防止脚本批量下单占用库存 —— 尤其是自动发货商品，下单即锁卡密',
    recommended: true,
  },
  {
    key: 'turnstile_on_order_query' as const,
    title: '订单查询',
    desc: '防止用订单号 + 邮箱撞库。凭免登录链接查单的买家不受影响',
    recommended: false,
  },
  {
    key: 'turnstile_on_coupon' as const,
    title: '优惠券校验',
    desc: '防止脚本枚举优惠码。开了之后买家用券时会多一步验证',
    recommended: false,
  },
]

/** 开了开关却没把两个密钥填齐 —— 保存会被后端拒绝 */
const captchaIncomplete = computed(
  () =>
    form.turnstile_enabled === '1' &&
    (!form.turnstile_site_key.trim() || !form.turnstile_secret_key.trim()),
)

/** 后台入口：保存前先在本地校验一次，把明显的错误挡在提交之前 */
const adminPathError = computed(() => {
  const v = form.admin_path.trim().toLowerCase().replace(/^\/+|\/+$/g, '')
  if (!v) return '入口路径不能为空'
  if (v === 'admin') return ''
  if (v.length < 8) return '自定义入口至少 8 位 —— 太短等于没改，几分钟就能被扫出来'
  if (v.length > 32) return '入口路径最长 32 位'
  if (!/^[a-z0-9][a-z0-9_-]*[a-z0-9]$/.test(v)) {
    return '只能包含小写字母、数字、中划线和下划线，且首尾必须是字母或数字'
  }
  if (RESERVED_PATHS.includes(v)) return '这个路径已被商城自身占用，请换一个'
  return ''
})

/** 与后端 model.reservedPaths 保持一致：占用了它们会让对应页面打不开 */
const RESERVED_PATHS = [
  'api', 'uploads', 'health', 'assets', 'setup', 'category', 'product',
  'checkout', 'pay', 'order', 'orders', 'favicon.svg', 'favicon.ico',
  'robots.txt', 'index.html', 'sitemap.xml', '.well-known', 'static',
  'public', 'docs',
]

/** 轮播图：同样在表单里用数组编辑 */
type BannerRow = { image: string; title: string; product_id: number }
const bannerRows = ref<BannerRow[]>([])
const bannerUploading = ref(-1)
/** 可绑定的商品，用于下拉。只列在售的 —— 绑一个下架商品等于绑了个死链 */
const productOptions = ref<{ label: string; value: string }[]>([])

async function loadProductOptions() {
  try {
    const all: { id: number; name: string }[] = []
    for (let page = 1; page <= 10; page++) {
      const res = await adminApi.products({ page, page_size: 100, status: 'on' })
      const rows = res.list ?? []
      all.push(...rows)
      if (rows.length < 100 || all.length >= res.total) break
    }
    productOptions.value = all.map((p) => ({ label: p.name, value: String(p.id) }))
  } catch {
    productOptions.value = []
  }
}

function addBanner() {
  if (bannerRows.value.length >= 8) {
    toast.warning('最多 8 张轮播图')
    return
  }
  bannerRows.value.push({ image: '', title: '', product_id: 0 })
}

function removeBanner(i: number) {
  bannerRows.value.splice(i, 1)
}

/** 上下移动。轮播的播放顺序就是数组顺序，能调整才有意义 */
function moveBanner(i: number, delta: number) {
  const j = i + delta
  if (j < 0 || j >= bannerRows.value.length) return
  const list = bannerRows.value
  ;[list[i], list[j]] = [list[j], list[i]]
}

async function onPickBanner(e: Event, i: number) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return

  bannerUploading.value = i
  try {
    const res = await adminApi.upload(file)
    bannerRows.value[i].image = res.url
    toast.success('图片已上传，别忘了保存')
  } catch (err) {
    toast.error(err instanceof ApiError ? err.message : '上传失败')
  } finally {
    bannerUploading.value = -1
  }
}

/** 联系方式：表单里用数组编辑，提交时序列化成 JSON */
type ContactRow = { type: string; value: string; label: string }
const contactRows = ref<ContactRow[]>([])

const CONTACT_TYPES = [
  { value: 'telegram', label: 'Telegram', action: '点击跳转', placeholder: '@yourname 或 https://t.me/yourname' },
  { value: 'whatsapp', label: 'WhatsApp', action: '点击跳转', placeholder: '8613800138000' },
  { value: 'wechat', label: '微信', action: '点击复制', placeholder: '微信号' },
  { value: 'qq', label: 'QQ', action: '点击复制', placeholder: 'QQ 号' },
  { value: 'email', label: '邮箱', action: '点击复制', placeholder: 'support@example.com' },
]

function contactMeta(t: string) {
  return CONTACT_TYPES.find((x) => x.value === t) ?? CONTACT_TYPES[0]
}

function addContact() {
  if (contactRows.value.length >= 10) {
    toast.warning('最多 10 条联系方式')
    return
  }
  contactRows.value.push({ type: 'wechat', value: '', label: '' })
}

function removeContact(i: number) {
  contactRows.value.splice(i, 1)
}

const savedAdminPath = ref('admin')
const adminPathChanged = computed(
  () => form.admin_path.trim().toLowerCase() !== savedAdminPath.value,
)
const adminUrlPreview = computed(
  () => `${location.origin}/${form.admin_path.trim().toLowerCase() || 'admin'}/login`,
)

const testToken = ref('')
const testing = ref(false)
const testResult = ref('')
const testOK = ref(false)
const testCaptchaRef = ref<InstanceType<typeof WdTurnstile> | null>(null)
const testTone = computed(() => (testOK.value ? 'text-[#4a9d9a]' : 'text-[#c17767]'))

/** 用已保存的密钥校验一次令牌，确认这对密钥真的能用 */
async function testTurnstile() {
  testing.value = true
  testResult.value = ''
  try {
    const res = await adminApi.testTurnstile(testToken.value)
    testOK.value = true
    // 用服务端的原话：它可能在提醒「这是测试密钥，会放行任何请求」
    testResult.value = res.message || '验证通过，这对密钥可以正常使用'
  } catch (e) {
    testOK.value = false
    testResult.value = e instanceof ApiError ? e.message : '测试失败'
  } finally {
    // 令牌一次性，测完就作废，换一张以便再试
    testCaptchaRef.value?.reset()
    testing.value = false
  }
}

const tabs = [
  { value: 'site', label: '站点信息' },
  { value: 'trade', label: '交易规则' },
  { value: 'seo', label: 'SEO 与页脚' },
  { value: 'security', label: '安全' },
]

/**
 * 只提交本页管理的 key。
 * 后端 settings 接口是「传什么改什么」，因此不能把整份配置回传 ——
 * 那样会把邮件页刚存的模板一起覆盖掉。
 */
const SITE_KEYS = [
  'site_name',
  'site_logo',
  'site_title',
  'site_description',
  'site_keywords',
  'site_notice',
  'site_footer',
  'icp',
  'currency',
  'currency_symbol',
  'timezone',
  'allow_order',
  'maintenance_mode',
  'maintenance_text',
  'order_expire_minutes',
  'turnstile_enabled',
  'turnstile_site_key',
  'turnstile_secret_key',
  'turnstile_on_admin_login',
  'turnstile_on_order',
  'turnstile_on_order_query',
  'turnstile_on_coupon',
  'turnstile_widget_size',
  'admin_path',
  'contacts',
  'banners',
  'notice_popup_enabled',
  'notice_force_read',
  'notice_force_seconds',
  'show_sales',
  'client_ip_headers',
] as const

type SettingKey = (typeof SITE_KEYS)[number]

const form = reactive<Record<SettingKey, string>>({
  site_name: '',
  site_logo: '',
  site_title: '',
  site_description: '',
  site_keywords: '',
  site_notice: '',
  site_footer: '',
  icp: '',
  currency: 'CNY',
  currency_symbol: '¥',
  timezone: 'Asia/Shanghai',
  turnstile_enabled: '0',
  turnstile_site_key: '',
  turnstile_secret_key: '',
  turnstile_on_admin_login: '1',
  turnstile_on_order: '0',
  turnstile_on_order_query: '0',
  turnstile_on_coupon: '0',
  turnstile_widget_size: 'normal',
  admin_path: 'admin',
  contacts: '[]',
  banners: '[]',
  notice_popup_enabled: '0',
  notice_force_read: '0',
  notice_force_seconds: '5',
  show_sales: '1',
  client_ip_headers: '',
  allow_order: '1',
  maintenance_mode: '0',
  maintenance_text: '',
  order_expire_minutes: '15',
})

const errors = reactive<Partial<Record<SettingKey, string>>>({})

/** 常用时区。列表之外的值也能保存，后端用 time.LoadLocation 校验。 */
const TIMEZONES = [
  'Asia/Shanghai',
  'Asia/Hong_Kong',
  'Asia/Taipei',
  'Asia/Tokyo',
  'Asia/Singapore',
  'Asia/Seoul',
  'Europe/London',
  'Europe/Berlin',
  'America/New_York',
  'America/Los_Angeles',
  'UTC',
].map((v) => ({ label: v, value: v }))

const CURRENCIES = [
  { label: 'CNY 人民币', value: 'CNY', symbol: '¥' },
  { label: 'USD 美元', value: 'USD', symbol: '$' },
  { label: 'EUR 欧元', value: 'EUR', symbol: '€' },
  { label: 'JPY 日元', value: 'JPY', symbol: '¥' },
  { label: 'HKD 港币', value: 'HKD', symbol: 'HK$' },
  { label: 'TWD 新台币', value: 'TWD', symbol: 'NT$' },
  { label: 'GBP 英镑', value: 'GBP', symbol: '£' },
]

const expireHint = computed(() => {
  const n = Number(form.order_expire_minutes)
  if (!Number.isFinite(n) || n < 60) return '超时未支付会自动取消并归还库存。'
  const hours = (n / 60).toFixed(n % 60 === 0 ? 0 : 1)
  return `约 ${hours} 小时。超时未支付会自动取消并归还库存。`
})

async function load() {
  loading.value = true
  try {
    const data = await adminApi.settings()
    for (const k of SITE_KEYS) {
      if (data[k] !== undefined) form[k] = data[k]
    }
    savedAdminPath.value = (form.admin_path || 'admin').trim().toLowerCase()

    try {
      const bs = JSON.parse(form.banners || '[]')
      bannerRows.value = Array.isArray(bs)
        ? bs.map((b: BannerRow) => ({
            image: b.image || '',
            title: b.title || '',
            product_id: Number(b.product_id) || 0,
          }))
        : []
    } catch {
      bannerRows.value = []
    }

    // 联系方式在库里是 JSON 串，这里摊成数组给表单编辑
    try {
      const parsed = JSON.parse(form.contacts || '[]')
      contactRows.value = Array.isArray(parsed)
        ? parsed.map((c: ContactRow) => ({
            type: c.type || 'wechat',
            value: c.value || '',
            label: c.label || '',
          }))
        : []
    } catch {
      // 坏数据不该让整个设置页打不开，清空重来即可
      contactRows.value = []
    }
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '加载配置失败')
  } finally {
    loading.value = false
  }
}

function validate(): boolean {
  for (const k of SITE_KEYS) delete errors[k]

  if (!form.site_name.trim()) errors.site_name = '商城名称不能为空'
  if (adminPathError.value) errors.admin_path = adminPathError.value

  const n = Number(form.order_expire_minutes)
  if (!Number.isInteger(n) || n < 1 || n > 1440) {
    errors.order_expire_minutes = '必须是 1-1440 之间的整数（分钟）'
  }
  if (!form.currency_symbol.trim()) errors.currency_symbol = '货币符号不能为空'
  if (!form.timezone.trim()) errors.timezone = '时区不能为空'

  return Object.keys(errors).length === 0
}

/** 切换币种时顺带带出默认符号，但不覆盖用户手填的符号。 */
function onCurrencyChange() {
  const c = CURRENCIES.find((x) => x.value === form.currency)
  if (!c) return
  const isDefault = CURRENCIES.some((x) => x.symbol === form.currency_symbol)
  if (!form.currency_symbol || isDefault) form.currency_symbol = c.symbol
}

async function onPickLogo(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = '' // 允许连续上传同一个文件
  if (!file) return

  uploading.value = true
  try {
    const res = await adminApi.upload(file)
    form.site_logo = res.url
    toast.success('Logo 已上传，别忘了保存')
  } catch (err) {
    toast.error(err instanceof ApiError ? err.message : '上传失败')
  } finally {
    uploading.value = false
  }
}

async function save() {
  if (!validate()) {
    // 定位到出错项所在的分页，否则用户只看到「保存失败」却找不到红字
    const bad = Object.keys(errors)[0] as SettingKey
    tab.value = bad === 'admin_path' ? 'security' : bad === 'order_expire_minutes' ? 'trade' : 'site'
    toast.error(errors[bad] ?? '请检查填写内容')
    return
  }

  saving.value = true
  try {
    const payload: Record<string, string> = {}
    for (const k of SITE_KEYS) payload[k] = String(form[k]).trim()
    // 内容为空的行直接丢掉：用户加了一行又没填，不该因此保存失败
    // 没选图的行直接丢掉：加了一行又没传图，不该因此保存失败
    payload.banners = JSON.stringify(
      bannerRows.value
        .filter((b) => b.image.trim())
        .map((b) => ({
          image: b.image.trim(),
          title: b.title.trim() || undefined,
          product_id: b.product_id || undefined,
        })),
    )
    payload.contacts = JSON.stringify(
      contactRows.value
        .filter((c) => c.value.trim())
        .map((c) => ({ type: c.type, value: c.value.trim(), label: c.label.trim() || undefined })),
    )
    await adminApi.updateSettings(payload)

    // 立刻刷新全局配置：站点名、货币符号在后台布局里就在用
    await shop.load(true).catch(() => undefined)

    const nextPath = payload.admin_path.toLowerCase() || 'admin'
    if (nextPath !== savedAdminPath.value) {
      // 入口变了：当前页面的后台路由是按旧路径注册的，留在原地会越点越错。
      // 整页跳到新地址，让服务端重新下发正确的入口。
      toast.success(`后台入口已改为 /${nextPath}，正在跳转…`)
      setTimeout(() => {
        location.href = `/${nextPath}/settings`
      }, 1200)
      return
    }
    toast.success('设置已保存')
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    saving.value = false
  }
}

/** 运行期事实：trust_proxy 开没开、内置头部有哪些、这次请求被认成了哪个 IP */
const runtime = ref<SettingsRuntime | null>(null)
const runtimeLoading = ref(false)

async function loadRuntime() {
  runtimeLoading.value = true
  try {
    runtime.value = await adminApi.settingsRuntime()
  } catch {
    // 拿不到就不显示那块，不用打扰站长 —— 这只是个自检面板
    runtime.value = null
  } finally {
    runtimeLoading.value = false
  }
}

onMounted(() => {
  load()
  loadProductOptions()
  loadRuntime()
})
</script>

<template>
  <div class="space-y-5">
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
      <WdTabs v-model="tab" :tabs="tabs" />
      <WdButton variant="primary" :loading="saving" :disabled="loading" @click="save">
        <Save class="w-4 h-4" />
        保存设置
      </WdButton>
    </div>

    <!-- 维护模式提醒放在最外层，切到哪个分页都看得到 -->
    <div
      v-if="form.maintenance_mode === '1'"
      class="flex items-start gap-2.5 px-5 py-4 rounded-2xl bg-[#e8b86d]/15 text-[#b8873f]"
    >
      <AlertTriangle class="w-4 h-4 mt-0.5 shrink-0" />
      <p class="text-sm leading-relaxed">
        维护模式已开启，前台商城对访客不可用（后台不受影响）。记得处理完后关掉。
      </p>
    </div>

    <!-- mode="out-in"：两个分页高度不同，交叉淡入会让页面跳一下 -->
    <Transition name="wd-tab" mode="out-in">
      <!-- 站点信息 -->
      <div v-if="tab === 'site'" key="site" class="space-y-5">
        <WdCard title="基础信息" subtitle="展示在前台页头、页面标题与通知邮件里">
          <div class="grid md:grid-cols-2 gap-5">
            <WdInput
              v-model="form.site_name"
              label="商城名称"
              required
              :maxlength="50"
              :error="errors.site_name"
              hint="显示在页头与邮件标题中"
            />
          </div>

          <div class="mt-5">
            <label class="block mb-1.5 text-xs font-medium text-gray-500">站点 Logo</label>
            <div class="flex items-center gap-4">
              <div
                class="w-16 h-16 rounded-xl bg-[#faf8f5] flex items-center justify-center overflow-hidden shrink-0"
              >
                <img
                  v-if="form.site_logo"
                  :src="form.site_logo"
                  alt="站点 Logo"
                  class="w-full h-full object-contain"
                />
                <ImagePlus v-else class="w-5 h-5 text-gray-300" />
              </div>

              <div class="flex-1 min-w-0 space-y-2">
                <WdInput v-model="form.site_logo" placeholder="图片 URL，留空则显示商城名称文字" />
                <div class="flex items-center gap-3">
                  <label
                    class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-[#faf8f5] text-xs font-medium text-gray-600 cursor-pointer hover:bg-[#f3efe9] transition-colors duration-200"
                  >
                    <Loader2 v-if="uploading" class="w-3.5 h-3.5 animate-spin" />
                    <ImagePlus v-else class="w-3.5 h-3.5" />
                    {{ uploading ? '上传中' : '上传图片' }}
                    <input
                      type="file"
                      accept="image/*"
                      class="hidden"
                      :disabled="uploading"
                      @change="onPickLogo"
                    />
                  </label>
                  <button
                    v-if="form.site_logo"
                    class="inline-flex items-center gap-1.5 text-xs font-medium text-[#c17767] hover:underline"
                    @click="form.site_logo = ''"
                  >
                    <Trash2 class="w-3.5 h-3.5" />
                    移除
                  </button>
                </div>
              </div>
            </div>
          </div>

          <div class="mt-5">
            <WdInput
              v-model="form.site_notice"
              type="textarea"
              label="首页公告"
              :rows="3"
              :maxlength="500"
              hint="留空则前台不显示公告条"
            />
          </div>
        </WdCard>

        <WdCard title="货币与时区" subtitle="影响价格展示与后台所有时间的显示">
          <div class="grid md:grid-cols-3 gap-5">
            <WdInput
              v-model="form.currency"
              type="select"
              label="币种"
              :options="CURRENCIES.map((c) => ({ label: c.label, value: c.value }))"
              @change="onCurrencyChange"
            />
            <WdInput
              v-model="form.currency_symbol"
              label="货币符号"
              required
              :maxlength="8"
              :error="errors.currency_symbol"
              hint="前台价格前缀"
            />
            <WdInput
              v-model="form.timezone"
              type="select"
              label="时区"
              required
              :options="TIMEZONES"
              :error="errors.timezone"
              hint="订单时间按此时区展示"
            />
          </div>
        </WdCard>
        <WdCard title="客服联系方式" subtitle="前台页脚会出现一个「联系客服」按钮，点开就是这些">
          <div class="space-y-3">
            <div
              v-for="(c, i) in contactRows"
              :key="i"
              class="flex flex-col sm:flex-row sm:items-end gap-3 pb-3"
              :class="i < contactRows.length - 1 && 'border-b border-gray-100'"
            >
              <div class="sm:w-36 shrink-0">
                <WdInput
                  v-model="c.type"
                  type="select"
                  :label="i === 0 ? '类型' : undefined"
                  :aria-label="`第 ${i + 1} 条的类型`"
                  :options="CONTACT_TYPES.map((t) => ({ label: t.label, value: t.value }))"
                />
              </div>
              <div class="flex-1 min-w-0">
                <WdInput
                  v-model="c.value"
                  :label="i === 0 ? '内容' : undefined"
                  :aria-label="`第 ${i + 1} 条的内容`"
                  :maxlength="200"
                  :placeholder="contactMeta(c.type).placeholder"
                />
              </div>
              <div class="sm:w-32 shrink-0">
                <WdInput
                  v-model="c.label"
                  :label="i === 0 ? '显示名（选填）' : undefined"
                  :aria-label="`第 ${i + 1} 条的显示名`"
                  :maxlength="20"
                  :placeholder="contactMeta(c.type).label"
                />
              </div>
              <div class="flex items-center gap-2 shrink-0">
                <span class="text-xs text-gray-400 whitespace-nowrap w-16">
                  {{ contactMeta(c.type).action }}
                </span>
                <button
                  type="button"
                  class="p-2 rounded-lg text-gray-300 hover:text-[#c17767] hover:bg-[#c17767]/10 transition-all duration-200"
                  :aria-label="`删除第 ${i + 1} 条联系方式`"
                  @click="removeContact(i)"
                >
                  <Trash2 class="w-4 h-4" />
                </button>
              </div>
            </div>

            <WdButton :disabled="contactRows.length >= 10" @click="addContact">
              <Plus class="w-4 h-4" />
              添加联系方式
            </WdButton>
            <p class="text-xs text-gray-500 leading-relaxed">
              Telegram 和 WhatsApp 会渲染成可点击的链接，其余（微信号、QQ 号、邮箱）
              点一下复制到剪贴板 —— 那才是用户拿到它之后真正要做的事。
            </p>
          </div>
        </WdCard>

        <WdCard title="首页轮播图" subtitle="首页最上方那块。一张就是静态图，多张自动轮播">
          <div class="space-y-3">
            <div
              v-for="(b, i) in bannerRows"
              :key="i"
              class="flex flex-col sm:flex-row gap-4 pb-4"
              :class="i < bannerRows.length - 1 && 'border-b border-gray-100'"
            >
              <!-- 预览。固定比例，和前台一致 -->
              <div
                class="w-full sm:w-56 shrink-0 aspect-[3/1] rounded-xl bg-[#faf8f5] border border-gray-200 overflow-hidden grid place-items-center"
              >
                <img v-if="b.image" :src="b.image" class="w-full h-full object-cover" alt="" />
                <ImagePlus v-else class="w-5 h-5 text-gray-300" />
              </div>

              <div class="flex-1 min-w-0 space-y-3">
                <div class="flex flex-wrap items-center gap-2">
                  <label
                    class="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-xl bg-white border border-gray-200 text-sm text-gray-600 cursor-pointer hover:border-[#4a9d9a]/40 hover:text-[#4a9d9a] transition-all duration-200"
                  >
                    <Loader2 v-if="bannerUploading === i" class="w-4 h-4 animate-spin" />
                    <ImagePlus v-else class="w-4 h-4" />
                    {{ b.image ? '换一张' : '上传图片' }}
                    <input
                      type="file"
                      accept="image/*"
                      class="hidden"
                      @change="(e) => onPickBanner(e, i)"
                    />
                  </label>
                  <button
                    type="button"
                    class="p-2 rounded-lg text-gray-300 hover:text-gray-600 hover:bg-gray-50 transition-all duration-200 disabled:opacity-30"
                    :disabled="i === 0"
                    :aria-label="`第 ${i + 1} 张上移`"
                    @click="moveBanner(i, -1)"
                  >
                    <ChevronUp class="w-4 h-4" />
                  </button>
                  <button
                    type="button"
                    class="p-2 rounded-lg text-gray-300 hover:text-gray-600 hover:bg-gray-50 transition-all duration-200 disabled:opacity-30"
                    :disabled="i === bannerRows.length - 1"
                    :aria-label="`第 ${i + 1} 张下移`"
                    @click="moveBanner(i, 1)"
                  >
                    <ChevronDown class="w-4 h-4" />
                  </button>
                  <button
                    type="button"
                    class="p-2 rounded-lg text-gray-300 hover:text-[#c17767] hover:bg-[#c17767]/10 transition-all duration-200"
                    :aria-label="`删除第 ${i + 1} 张`"
                    @click="removeBanner(i)"
                  >
                    <Trash2 class="w-4 h-4" />
                  </button>
                </div>

                <div class="grid sm:grid-cols-2 gap-3">
                  <WdInput
                    v-model="b.image"
                    :aria-label="`第 ${i + 1} 张的图片地址`"
                    placeholder="图片地址（也可直接填外链）"
                    :maxlength="500"
                  />
                  <WdInput
                    v-model="b.title"
                    :aria-label="`第 ${i + 1} 张的说明文字`"
                    placeholder="说明文字（选填，压在图片下方）"
                    :maxlength="60"
                  />
                </div>

                <WdInput
                  :model-value="b.product_id ? String(b.product_id) : ''"
                  type="select"
                  label="点击后跳转到"
                  placeholder="不跳转"
                  clearable
                  :options="productOptions"
                  hint="只列出在售商品。绑定后整张图可点；不绑就只是一张图"
                  @update:model-value="(v: string | number | null) => (b.product_id = Number(v) || 0)"
                />
              </div>
            </div>

            <WdButton :disabled="bannerRows.length >= 8" @click="addBanner">
              <Plus class="w-4 h-4" />
              添加轮播图
            </WdButton>
            <p class="text-xs text-gray-500 leading-relaxed">
              建议尺寸 1200×400（3:1）。一张时静态展示；多张时每 5 秒自动切换，
              可用箭头、圆点或手指滑动切换。绑定的商品如果下架或删除，那张图会自动变成不可点，
              不会把访客带到 404。
            </p>
          </div>
        </WdCard>

        <WdCard title="公告展示" subtitle="控制公告怎么呈现给访客">
          <div class="space-y-4">
            <div class="flex items-start justify-between gap-6">
              <div class="min-w-0">
                <p class="text-sm font-medium text-gray-800">公告用弹窗展示</p>
                <p class="mt-1 text-xs text-gray-500 leading-relaxed">
                  写在页面里的公告很容易被划过去。开启后进站会弹一次；
                  公告内容不变就不再打扰，改了内容所有人会重新看到
                </p>
              </div>
              <WdSwitch
                :model-value="form.notice_popup_enabled === '1'"
                label="公告弹窗"
                @update:model-value="(v) => (form.notice_popup_enabled = v ? '1' : '0')"
              />
            </div>

            <div
              v-if="form.notice_popup_enabled === '1'"
              class="pt-4 border-t border-gray-100 space-y-4"
            >
              <div class="flex items-start justify-between gap-6">
                <div class="min-w-0">
                  <p class="text-sm font-medium text-gray-800">强制阅读</p>
                  <p class="mt-1 text-xs text-gray-500 leading-relaxed">
                    倒计时结束前关不掉（× 和 Esc 都不生效）。用于必读条款，别滥用
                  </p>
                </div>
                <WdSwitch
                  :model-value="form.notice_force_read === '1'"
                  label="强制阅读"
                  @update:model-value="(v) => (form.notice_force_read = v ? '1' : '0')"
                />
              </div>
              <div v-if="form.notice_force_read === '1'" class="max-w-xs">
                <WdInput
                  v-model="form.notice_force_seconds"
                  type="number"
                  label="强制阅读时长（秒）"
                  :min="1"
                  :max="60"
                  hint="1-60 秒"
                />
              </div>
            </div>

            <p
              v-if="form.notice_popup_enabled === '1' && !form.site_notice.trim()"
              class="px-3.5 py-2.5 rounded-xl bg-[#e8b86d]/15 text-xs text-[#8f7243] leading-relaxed"
            >
              公告内容为空，弹窗不会出现。请先在上方「首页公告」里写点什么。
            </p>
          </div>
        </WdCard>

        <WdCard title="商品展示" subtitle="控制商品卡片和详情页上露出哪些信息">
          <div class="flex items-start justify-between gap-6">
            <div class="min-w-0">
              <p class="text-sm font-medium text-gray-800">显示已售数量</p>
              <p class="mt-1 text-xs text-gray-500 leading-relaxed">
                商品卡片上的「已售 N」和详情页的「累计销量」。关掉之后接口也不再下发这个数字，
                不是只在页面上藏起来。新店销量还小的时候，摆出来反而劝退
              </p>
            </div>
            <WdSwitch
              :model-value="form.show_sales === '1'"
              label="显示已售数量"
              @update:model-value="(v) => (form.show_sales = v ? '1' : '0')"
            />
          </div>
        </WdCard>
      </div>

      <!-- 交易规则 -->
      <div v-else-if="tab === 'trade'" key="trade" class="space-y-5">
        <WdCard title="下单与订单" subtitle="控制用户能否下单，以及未付款订单何时释放库存">
          <div class="space-y-5">
            <div class="flex items-start justify-between gap-6 pb-5 border-b border-gray-100">
              <div class="min-w-0">
                <p class="text-sm font-medium text-gray-800">允许下单</p>
                <p class="mt-1 text-xs text-gray-400 leading-relaxed">
                  关闭后前台商品可以浏览，但无法提交订单。适合临时停售。
                </p>
              </div>
              <WdSwitch
                :model-value="form.allow_order === '1'"
                label="允许下单"
                @update:model-value="(v) => (form.allow_order = v ? '1' : '0')"
              />
            </div>

            <div class="max-w-xs">
              <WdInput
                v-model="form.order_expire_minutes"
                type="number"
                label="订单超时时间（分钟）"
                required
                :min="1"
                :max="1440"
                :error="errors.order_expire_minutes"
                :hint="expireHint"
              />
            </div>

            <p class="px-3.5 py-2.5 rounded-xl bg-[#faf8f5] text-xs text-gray-500 leading-relaxed">
              下单时就会占用库存（卡密会被预留），所以这个值不宜过长 ——
              设成几小时的话，一批放弃支付的订单会长时间锁住卡密。推荐 15-30 分钟。
            </p>
          </div>
        </WdCard>

        <WdCard title="维护模式" subtitle="临时关闭前台，后台仍可正常访问">
          <div class="space-y-5">
            <div class="flex items-start justify-between gap-6">
              <div class="min-w-0">
                <p class="text-sm font-medium text-gray-800">开启维护模式</p>
                <p class="mt-1 text-xs text-gray-400 leading-relaxed">
                  访客访问前台时只会看到下面这段提示文字。
                </p>
              </div>
              <WdSwitch
                :model-value="form.maintenance_mode === '1'"
                label="开启维护模式"
                @update:model-value="(v) => (form.maintenance_mode = v ? '1' : '0')"
              />
            </div>

            <WdInput
              v-model="form.maintenance_text"
              type="textarea"
              label="维护提示文字"
              :rows="2"
              :maxlength="300"
              placeholder="商城正在维护升级，请稍后再来。"
            />
          </div>
        </WdCard>
      </div>

      <!-- SEO -->
      <div v-else-if="tab === 'seo'" key="seo" class="space-y-5">
        <WdCard title="搜索引擎信息" subtitle="写入页面 title 与 meta 标签">
          <div class="space-y-5">
            <WdInput
              v-model="form.site_title"
              label="页面标题"
              :maxlength="80"
              hint="浏览器标签页与搜索结果标题。留空则使用商城名称"
            />
            <WdInput
              v-model="form.site_description"
              type="textarea"
              label="站点描述"
              :rows="2"
              :maxlength="200"
              hint="meta description，建议 60-150 字"
            />
            <WdInput
              v-model="form.site_keywords"
              label="关键词"
              :maxlength="200"
              placeholder="卡密,自动发货,数字商品"
              hint="英文逗号分隔"
            />
          </div>
        </WdCard>

        <WdCard title="页脚" subtitle="展示在前台每一页的底部">
          <div class="space-y-5">
            <WdInput
              v-model="form.site_footer"
              type="textarea"
              label="页脚文字"
              :rows="2"
              :maxlength="300"
              hint="版权声明、免责声明等"
            />
            <WdInput
              v-model="form.icp"
              label="备案号"
              :maxlength="60"
              placeholder="京ICP备00000000号-1"
              hint="中国大陆服务器需要展示"
            />
          </div>
        </WdCard>
      </div>

      <!-- 安全：后台入口 + 人机验证 -->
      <div v-else key="security" class="space-y-5">
        <WdCard title="后台入口地址" subtitle="改掉默认的 /admin，扫描器就撞不到登录页">
          <div class="space-y-4">
            <WdInput
              v-model="form.admin_path"
              label="入口路径"
              mono
              :maxlength="32"
              placeholder="admin"
              hint="只能用小写字母、数字、中划线、下划线；自定义时至少 8 位"
            />
            <p class="px-3.5 py-2.5 rounded-xl bg-[#faf8f5] text-xs text-gray-500 leading-relaxed">
              保存后后台地址变成
              <span class="font-mono text-gray-700">{{ adminUrlPreview }}</span>
              ，请先收藏好再保存。
            </p>
            <div
              v-if="adminPathChanged"
              class="flex items-start gap-2.5 px-4 py-3.5 rounded-xl bg-[#e8b86d]/15 text-[#8f7243]"
            >
              <AlertTriangle class="w-4 h-4 mt-0.5 shrink-0" />
              <p class="text-sm leading-relaxed">
                保存后<span class="font-medium">旧地址会立刻失效</span>，当前页面也会跳到新地址。
                忘了新地址时，可以用 SQLite 工具打开 data\moecard.db，
                查 system_settings 表里的 admin_path。
              </p>
            </div>
            <p v-if="adminPathError" class="text-xs text-[#c17767] leading-relaxed">
              {{ adminPathError }}
            </p>
          </div>
        </WdCard>

        <WdCard>
          <div class="flex items-start justify-between gap-6">
            <div class="min-w-0">
              <div class="flex items-center gap-2">
                <ShieldCheck class="w-4 h-4 text-[#4a9d9a]" />
                <p class="text-sm font-medium text-gray-800">启用 Cloudflare Turnstile</p>
                <WdBadge v-if="captchaIncomplete" tone="amber">密钥未填完</WdBadge>
              </div>
              <p class="mt-1.5 text-xs text-gray-500 leading-relaxed">
                在关键操作前加一道人机校验，挡住脚本刷单、撞库和薅券。
                对真人几乎无感（多数情况下不需要点任何东西），且完全免费。
              </p>
            </div>
            <WdSwitch
              :model-value="form.turnstile_enabled === '1'"
              label="启用人机验证"
              @update:model-value="(v) => (form.turnstile_enabled = v ? '1' : '0')"
            />
          </div>

          <div
            v-if="form.turnstile_enabled === '1'"
            class="mt-5 flex items-start gap-2.5 px-4 py-3.5 rounded-xl bg-[#e8b86d]/15 text-[#8f7243]"
          >
            <AlertTriangle class="w-4 h-4 mt-0.5 shrink-0" />
            <p class="text-sm leading-relaxed">
              密钥填错会把<span class="font-medium">所有人挡在门外，包括你自己</span>——
              后台登录页也会进不去。保存前请先用下方的「测试配置」验证一次。
            </p>
          </div>
        </WdCard>

        <WdCard title="密钥" subtitle="在 Cloudflare 控制台 → Turnstile 新建一个站点后获得">
          <div class="space-y-5">
            <WdInput
              v-model="form.turnstile_site_key"
              label="站点密钥 Site Key"
              placeholder="0x4AAAAAAA..."
              mono
              hint="公开信息，会写进页面。测试可用官方 1x00000000000000000000AA（永远通过）"
            />
            <WdInput
              v-model="form.turnstile_secret_key"
              type="password"
              label="密钥 Secret Key"
              placeholder="0x4AAAAAAA..."
              mono
              hint="服务端校验用，绝不会出现在前台。保存后只显示脱敏值，不改动即保留原值"
            />
            <div class="max-w-xs">
              <WdInput
                v-model="form.turnstile_widget_size"
                type="select"
                label="控件尺寸"
                :options="[
                  { label: '标准（normal）', value: 'normal' },
                  { label: '自适应宽度（flexible）', value: 'flexible' },
                  { label: '紧凑（compact）', value: 'compact' },
                ]"
              />
            </div>

            <div class="pt-1 border-t border-gray-100">
              <p class="mt-4 text-xs text-gray-500 leading-relaxed">
                填好密钥并<span class="font-medium text-gray-700">先保存</span>，然后在下面完成一次验证，
                确认这对密钥能正常工作。
              </p>
              <div class="mt-3 space-y-3">
                <WdTurnstile
                  ref="testCaptchaRef"
                  v-model="testToken"
                  scene="admin_login"
                  :force="true"
                  :site-key="form.turnstile_site_key"
                />
                <div class="flex items-center gap-3">
                  <WdButton :loading="testing" :disabled="!testToken" @click="testTurnstile">
                    <ShieldCheck class="w-4 h-4" />
                    测试配置
                  </WdButton>
                  <p v-if="testResult" class="text-xs leading-relaxed" :class="testTone">
                    {{ testResult }}
                  </p>
                </div>
              </div>
            </div>
          </div>
        </WdCard>

        <WdCard title="在哪些地方验证" subtitle="按需开关，只在真正需要防刷的入口打开">
          <div class="space-y-4">
            <div
              v-for="(sc, i) in CAPTCHA_SCENES"
              :key="sc.key"
              class="flex items-start justify-between gap-6"
              :class="i < CAPTCHA_SCENES.length - 1 && 'pb-4 border-b border-gray-100'"
            >
              <div class="min-w-0">
                <div class="flex items-center gap-2">
                  <p class="text-sm font-medium text-gray-800">{{ sc.title }}</p>
                  <WdBadge v-if="sc.recommended" tone="teal">建议开启</WdBadge>
                </div>
                <p class="mt-1 text-xs text-gray-500 leading-relaxed">{{ sc.desc }}</p>
              </div>
              <WdSwitch
                :model-value="form[sc.key] === '1'"
                :label="sc.title"
                :disabled="form.turnstile_enabled !== '1'"
                @update:model-value="(v) => (form[sc.key] = v ? '1' : '0')"
              />
            </div>
          </div>
          <p
            v-if="form.turnstile_enabled !== '1'"
            class="mt-4 px-3.5 py-2.5 rounded-xl bg-[#faf8f5] text-xs text-gray-500"
          >
            总开关关闭时这些选项不生效。
          </p>
        </WdCard>

        <WdCard title="客户端 IP 识别" subtitle="决定限流、人机验证和下单记录里看到的是谁的 IP">
          <div class="space-y-4">
            <div
              v-if="runtime && !runtime.trust_proxy"
              class="flex items-start gap-2.5 px-4 py-3.5 rounded-xl bg-[#e8b86d]/15 text-[#8f7243]"
            >
              <AlertTriangle class="w-4 h-4 mt-0.5 shrink-0" />
              <p class="text-xs leading-relaxed">
                当前没有开启 <span class="font-mono">TRUST_PROXY</span>，
                所有 IP 请求头都会被忽略，这里填了也不生效。
                如果你的站点在 CDN 或反向代理后面，请在
                <span class="font-mono">.env</span> 里设置
                <span class="font-mono">TRUST_PROXY=true</span> 并重启。
                直接暴露在公网时请保持关闭 —— 否则任何人都能伪造自己的 IP，
                限流和风控会形同虚设。
              </p>
            </div>

            <WdInput
              v-model="form.client_ip_headers"
              type="textarea"
              label="自定义客户端 IP 请求头"
              mono
              :rows="3"
              placeholder="X-Client-Real-IP&#10;My-CDN-Client-IP"
              hint="一行一个，也可以用逗号分隔；最多 5 个。解析时优先于下面的内置请求头"
            />

            <div class="px-4 py-3.5 rounded-xl bg-[#faf8f5] space-y-2">
              <p class="text-xs text-gray-500 leading-relaxed">
                内置请求头（按顺序尝试，命中第一个就停）：
              </p>
              <div class="flex flex-wrap gap-1.5">
                <span
                  v-for="hname in runtime?.builtin_ip_headers || []"
                  :key="hname"
                  class="px-2 py-1 rounded-lg bg-white border border-gray-200 text-[11px] font-mono text-gray-600"
                >
                  {{ hname }}
                </span>
              </div>
              <p class="text-xs text-gray-500 leading-relaxed">
                已覆盖 Cloudflare、腾讯云 EdgeOne、阿里云 ESA / CDN、Akamai
                以及常见的 Nginx 反代写法。用别家 CDN 或自建反代时，把它的头部名填在上面。
              </p>
            </div>

            <div
              v-if="runtime"
              class="px-4 py-3.5 rounded-xl border border-gray-200 flex flex-wrap items-center gap-x-6 gap-y-2"
            >
              <div>
                <p class="text-xs text-gray-500">当前识别到你的 IP</p>
                <p class="mt-0.5 text-sm font-mono text-gray-800">{{ runtime.detected_ip || '—' }}</p>
              </div>
              <div>
                <p class="text-xs text-gray-500">来自</p>
                <p class="mt-0.5 text-sm font-mono text-gray-800">
                  {{ runtime.detected_from || '直连地址（没有命中任何请求头）' }}
                </p>
              </div>
              <WdButton class="ml-auto" :loading="runtimeLoading" @click="loadRuntime">
                重新检测
              </WdButton>
            </div>
          </div>
        </WdCard>
      </div>
    </Transition>
  </div>
</template>
