<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  BellRing,
  ChevronRight,
  CreditCard,
  ExternalLink,
  FileClock,
  ImagePlus,
  Info,
  KeyRound,
  Loader2,
  LayoutDashboard,
  Mail,
  Package,
  PanelLeft,
  Receipt,
  Settings,
  Tags,
  Ticket,
  Users,
} from 'lucide-vue-next'
import { ApiError, adminApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { useShopStore } from '@/stores/shop'
import { WdButton, WdInput, WdModal, confirmDialog, toast } from '@/ui'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const shop = useShopStore()

const sidebarOpen = ref(true)
const mobileOpen = ref(false)
const userMenuOpen = ref(false)

const groups = [
  {
    label: '概览',
    items: [{ name: 'admin-dashboard', title: '控制台', icon: LayoutDashboard }],
  },
  {
    label: '商品',
    items: [
      { name: 'admin-products', title: '商品管理', icon: Package },
      { name: 'admin-categories', title: '商品分类', icon: Tags },
      { name: 'admin-codes', title: '卡密管理', icon: KeyRound },
    ],
  },
  {
    label: '交易',
    items: [
      { name: 'admin-orders', title: '订单管理', icon: Receipt },
      { name: 'admin-coupons', title: '优惠券', icon: Ticket },
    ],
  },
  {
    label: '系统',
    items: [
      { name: 'admin-payments', title: '支付方式', icon: CreditCard },
      { name: 'admin-mail', title: '邮件配置', icon: Mail },
      { name: 'admin-notify', title: '商家通知', icon: BellRing },
      { name: 'admin-settings', title: '商城设置', icon: Settings },
      { name: 'admin-admins', title: '管理员', icon: Users },
      { name: 'admin-logs', title: '系统日志', icon: FileClock },
      { name: 'admin-about', title: '关于', icon: Info },
    ],
  },
]

const meta = computed(() => ({
  title: (route.meta.title as string) || '后台',
  subtitle: (route.meta.subtitle as string) || '',
}))

const initial = computed(() => (auth.admin?.username || 'A').slice(0, 1).toUpperCase())

// ---- 修改资料（昵称 + 头像）----
const profileVisible = ref(false)
const profileForm = ref({ nickname: '', avatar: '' })
const profileSaving = ref(false)
const avatarUploading = ref(false)

function openProfileDialog() {
  userMenuOpen.value = false
  profileForm.value = {
    nickname: auth.admin?.nickname || '',
    avatar: auth.admin?.avatar || '',
  }
  profileVisible.value = true
}

async function onPickAvatar(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  avatarUploading.value = true
  try {
    const res = await adminApi.upload(file)
    profileForm.value.avatar = res.url
  } catch {
    toast.error('头像上传失败')
  } finally {
    avatarUploading.value = false
    // 清空后同一张图能再选一次，否则第二次选它不会触发 change
    input.value = ''
  }
}

async function saveProfile() {
  profileSaving.value = true
  try {
    const updated = await adminApi.updateProfile({
      nickname: profileForm.value.nickname,
      avatar: profileForm.value.avatar,
    })
    auth.admin = updated
    profileVisible.value = false
    toast.success('资料已更新')
  } catch (err) {
    toast.error(err instanceof Error ? err.message : '保存失败')
  } finally {
    profileSaving.value = false
  }
}

const pwdVisible = ref(false)

function openPasswordDialog() {
  userMenuOpen.value = false
  pwdVisible.value = true
}
const pwdForm = ref({ old_password: '', new_password: '', confirm: '' })
const pwdSubmitting = ref(false)

async function changePassword() {
  if (pwdForm.value.new_password !== pwdForm.value.confirm) {
    toast.error('两次输入的新密码不一致')
    return
  }
  if (pwdForm.value.new_password.length < 8) {
    toast.error('新密码至少 8 位')
    return
  }
  pwdSubmitting.value = true
  try {
    await adminApi.changePassword(pwdForm.value.old_password, pwdForm.value.new_password)
    toast.success('密码已修改，请重新登录')
    pwdVisible.value = false
    // 改密码会让所有已签发的 token 失效，必须重新登录
    auth.reset()
    router.replace({ name: 'admin-login' })
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '修改失败')
  } finally {
    pwdSubmitting.value = false
  }
}

async function logout() {
  userMenuOpen.value = false
  if (!(await confirmDialog({ message: '确定要退出登录吗？', confirmText: '退出' }))) return
  await auth.logout()
  router.replace({ name: 'admin-login' })
}

function go(name: string) {
  mobileOpen.value = false
  router.push({ name })
}
</script>

<template>
  <div class="min-h-screen bg-[#faf8f5] text-gray-800">
    <!-- 侧栏 -->
    <aside
      class="fixed top-0 left-0 h-full z-40 bg-[#faf8f5] border-r border-gray-200/60 overflow-hidden transition-all duration-300"
      :class="[sidebarOpen ? 'lg:w-60' : 'lg:w-0', mobileOpen ? 'w-60' : 'w-0']"
    >
      <div class="w-60 h-full p-5 flex flex-col">
        <div class="flex items-center gap-3 mb-8 px-1">
          <!-- 配了站点 logo 就用站点 logo：后台和前台是同一个商城，
               侧栏顶上摆一个通用图标只会让人多想一秒"这是哪个站" -->
          <img
            v-if="shop.config.site_logo"
            :src="shop.config.site_logo"
            :alt="shop.config.site_name"
            class="w-9 h-9 rounded-xl object-cover shrink-0"
          />
          <img v-else src="/icon-192.png" alt="MoeCard" class="w-9 h-9 rounded-xl shrink-0" />
          <span class="font-semibold text-base text-gray-800 truncate">
            {{ shop.config.site_name }}
          </span>
        </div>

        <nav class="flex-1 overflow-y-auto no-scrollbar space-y-1">
          <template v-for="g in groups" :key="g.label">
            <p class="px-4 pt-4 pb-1.5 text-[11px] tracking-wider text-gray-300">{{ g.label }}</p>
            <button
              v-for="m in g.items"
              :key="m.name"
              class="w-full flex items-center gap-3 px-4 py-2.5 rounded-xl text-sm transition-all duration-200"
              :class="
                route.name === m.name
                  ? 'bg-[#4a9d9a] text-white shadow-lg shadow-[#4a9d9a]/25'
                  : 'text-gray-500 hover:bg-white hover:text-gray-800 hover:shadow-sm'
              "
              @click="go(m.name)"
            >
              <component :is="m.icon" class="w-[18px] h-[18px] shrink-0" />
              <span class="font-medium truncate">{{ m.title }}</span>
              <ChevronRight v-if="route.name === m.name" class="w-3.5 h-3.5 ml-auto opacity-60" />
            </button>
          </template>
        </nav>

        <div class="mt-4 pt-4 border-t border-gray-100">
          <a
            href="/"
            target="_blank"
            rel="noopener"
            class="w-full flex items-center gap-3 px-4 py-2.5 rounded-xl text-sm text-gray-500 hover:bg-white hover:text-gray-800 transition-all duration-200"
          >
            <ExternalLink class="w-[18px] h-[18px] shrink-0" />
            <span class="font-medium">访问商城</span>
          </a>
        </div>
      </div>
    </aside>

    <div
      v-if="mobileOpen"
      class="fixed inset-0 z-30 bg-gray-900/20 lg:hidden"
      @click="mobileOpen = false"
    />

    <!-- 主区 -->
    <div class="transition-all duration-300" :class="sidebarOpen ? 'lg:ml-60' : 'lg:ml-0'">
      <header
        class="sticky top-0 z-20 bg-[#faf8f5]/85 backdrop-blur-md border-b border-gray-200/50"
      >
        <div class="flex items-center justify-between gap-4 px-5 md:px-8 py-3.5">
          <div class="flex items-center gap-3 min-w-0">
            <button
              class="p-2 rounded-xl text-gray-500 hover:bg-white hover:shadow-sm transition-all duration-200 lg:hidden"
              aria-label="打开菜单"
              @click="mobileOpen = true"
            >
              <PanelLeft class="w-5 h-5" />
            </button>
            <button
              class="hidden lg:block p-2 rounded-xl text-gray-500 hover:bg-white hover:shadow-sm transition-all duration-200"
              aria-label="折叠侧栏"
              @click="sidebarOpen = !sidebarOpen"
            >
              <PanelLeft class="w-5 h-5" />
            </button>
            <div class="min-w-0">
              <h1 class="text-lg font-semibold text-gray-800 truncate">{{ meta.title }}</h1>
              <p v-if="meta.subtitle" class="text-xs text-gray-400 truncate">{{ meta.subtitle }}</p>
            </div>
          </div>

          <div class="relative shrink-0">
            <button
              class="flex items-center gap-2.5 pl-2.5 pr-1 py-1 rounded-xl hover:bg-white hover:shadow-sm transition-all duration-200"
              @click="userMenuOpen = !userMenuOpen"
            >
              <span class="hidden sm:block text-sm text-gray-500 max-w-24 truncate">
                {{ auth.admin?.nickname || auth.admin?.username }}
              </span>
              <img
                v-if="auth.admin?.avatar"
                :src="auth.admin.avatar"
                :alt="auth.admin?.nickname || auth.admin?.username"
                class="w-9 h-9 rounded-xl object-cover shrink-0"
              />
              <span
                v-else
                class="w-9 h-9 rounded-xl bg-[#e8b86d] grid place-items-center text-white text-sm font-semibold shrink-0"
              >
                {{ initial }}
              </span>
            </button>

            <div v-if="userMenuOpen" class="fixed inset-0 z-30" @click="userMenuOpen = false" />

            <Transition
              enter-active-class="transition-all duration-200 ease-out"
              enter-from-class="opacity-0 -translate-y-1"
              leave-active-class="transition-all duration-150 ease-in"
              leave-to-class="opacity-0 -translate-y-1"
            >
              <div
                v-if="userMenuOpen"
                class="absolute right-0 top-full mt-2 w-52 z-40 bg-white rounded-2xl shadow-xl shadow-black/[0.08] border border-gray-100 overflow-hidden"
              >
                <div class="px-4 py-3 border-b border-gray-100">
                  <p class="text-sm font-medium text-gray-800 truncate">
                    {{ auth.admin?.username }}
                  </p>
                  <p class="text-xs text-gray-400 truncate">
                    最近登录 {{ auth.admin?.last_login_ip || '—' }}
                  </p>
                </div>
                <button
                  class="w-full px-4 py-2.5 text-left text-sm text-gray-600 hover:bg-[#faf8f5] transition-colors duration-200"
                  @click="openProfileDialog"
                >
                  修改资料
                </button>
                <button
                  class="w-full px-4 py-2.5 text-left text-sm text-gray-600 hover:bg-[#faf8f5] transition-colors duration-200"
                  @click="openPasswordDialog"
                >
                  修改密码
                </button>
                <button
                  class="w-full px-4 py-2.5 text-left text-sm text-[#c17767] hover:bg-[#faf8f5] transition-colors duration-200"
                  @click="logout"
                >
                  退出登录
                </button>
              </div>
            </Transition>
          </div>
        </div>
      </header>

      <main class="p-5 md:p-8">
        <RouterView v-slot="{ Component }">
          <component :is="Component" />
        </RouterView>
      </main>
    </div>

    <!-- 修改资料 -->
    <WdModal v-model="profileVisible" title="修改资料" width="420px">
      <div class="space-y-4">
        <div class="flex items-center gap-4">
          <img
            v-if="profileForm.avatar"
            :src="profileForm.avatar"
            alt="头像预览"
            class="w-16 h-16 rounded-2xl object-cover shrink-0 border border-gray-200"
          />
          <span
            v-else
            class="w-16 h-16 rounded-2xl bg-[#e8b86d] grid place-items-center text-white text-xl font-semibold shrink-0"
          >
            {{ initial }}
          </span>
          <div class="flex flex-wrap gap-2">
            <label
              class="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-xl bg-white border border-gray-200 text-sm text-gray-600 cursor-pointer hover:border-[#4a9d9a]/40 hover:text-[#4a9d9a] transition-all duration-200"
            >
              <Loader2 v-if="avatarUploading" class="w-4 h-4 animate-spin" />
              <ImagePlus v-else class="w-4 h-4" />
              {{ profileForm.avatar ? '换一张' : '上传头像' }}
              <input type="file" accept="image/*" class="hidden" @change="onPickAvatar" />
            </label>
            <WdButton v-if="profileForm.avatar" @click="profileForm.avatar = ''">
              移除
            </WdButton>
          </div>
        </div>

        <WdInput
          v-model="profileForm.avatar"
          label="头像地址"
          placeholder="上传后自动填入，也可以直接填外链"
          :maxlength="255"
        />
        <WdInput
          v-model="profileForm.nickname"
          label="昵称"
          placeholder="留空则显示用户名"
          :maxlength="60"
          hint="只影响后台右上角的显示，登录仍然用用户名"
        />
      </div>
      <template #footer>
        <WdButton @click="profileVisible = false">取消</WdButton>
        <WdButton variant="primary" :loading="profileSaving" @click="saveProfile">保存</WdButton>
      </template>
    </WdModal>

    <!-- 修改密码 -->
    <WdModal v-model="pwdVisible" title="修改密码" width="420px" :close-on-overlay="false">
      <div class="space-y-4">
        <WdInput v-model="pwdForm.old_password" type="password" label="原密码" />
        <WdInput
          v-model="pwdForm.new_password"
          type="password"
          label="新密码"
          hint="至少 8 位，需包含字母、数字、符号中的两类"
        />
        <WdInput v-model="pwdForm.confirm" type="password" label="确认新密码" />
        <p class="px-3.5 py-2.5 rounded-xl bg-[#faf8f5] text-xs text-gray-500 leading-relaxed">
          修改成功后所有已登录设备都会失效，需要重新登录。
        </p>
      </div>
      <template #footer>
        <WdButton @click="pwdVisible = false">取消</WdButton>
        <WdButton variant="primary" :loading="pwdSubmitting" @click="changePassword">确定</WdButton>
      </template>
    </WdModal>
  </div>
</template>
