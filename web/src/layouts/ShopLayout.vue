<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Github, Menu, Search, X } from 'lucide-vue-next'
import { PROJECT_URL } from '@/constants'
import ContactMenu from '@/components/ContactMenu.vue'
import NoticeDialog from '@/components/NoticeDialog.vue'
import { useContacts } from '@/composables/useContacts'
import type { ShopContact } from '@/api/types'
import { useShopStore } from '@/stores/shop'

const shop = useShopStore()
const { contacts, iconOf, actionIconOf, pick } = useContacts()

/** 移动端菜单里点一条联系方式：处理完顺手把菜单收起来 */
async function pickAndClose(c: ShopContact) {
  await pick(c)
  menuOpen.value = false
}
const router = useRouter()
const route = useRoute()

const keyword = ref((route.query.keyword as string) || '')
const menuOpen = ref(false)

/**
 * 边打边搜，不用按回车。
 *
 * 300ms 大致是"打完了"和"还在打"的分界：再短会在词打到一半时就发请求，
 * 再长就能感觉到迟滞。清空输入框等于没有关键词，回到全部商品。
 */
const SEARCH_DEBOUNCE_MS = 300
let timer: ReturnType<typeof setTimeout> | undefined

function go() {
  const q = keyword.value.trim()
  const query = q ? { keyword: q } : {}
  // 已经在首页就用 replace：边打边搜每个字都留一条历史记录的话，
  // 用户想退回上一页得连按十几次返回键。
  // 从别的页面过来则用 push，保证返回键能回到商品页。
  if (route.name === 'home') router.replace({ name: 'home', query })
  else router.push({ name: 'home', query })
}

/**
 * 只在真的有人敲键盘时才排队搜索。
 *
 * 不用 watch(keyword)：程序把输入框同步成地址栏的值时它也会触发，
 * 于是"退回商品页 → 框被清空 → 防抖把人推回首页"，返回键就废了。
 * input 事件只有用户输入（含输入框自带的清除按钮）才会发出。
 */
function onType() {
  clearTimeout(timer)
  timer = setTimeout(go, SEARCH_DEBOUNCE_MS)
}

/** 回车立即搜，不等防抖；移动端顺手把菜单收起来 */
function search() {
  clearTimeout(timer)
  go()
  menuOpen.value = false
}

// 地址栏被别处改了（比如商品列表里的"清空筛选"）时，把输入框同步过来。
// 比较用 trim 后的值，免得和上面的 watch 互相触发。
watch(
  () => route.query.keyword,
  (v) => {
    const s = (v as string) || ''
    if (s !== keyword.value.trim()) keyword.value = s
  },
)

onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <div class="min-h-screen flex flex-col bg-[#faf8f5]">
    <header class="sticky top-0 z-40 bg-white/90 backdrop-blur-md shadow-sm shadow-black/[0.03]">
      <div class="max-w-6xl mx-auto px-5 sm:px-6 lg:px-8">
        <div class="flex items-center gap-4 md:gap-8 h-16">
          <RouterLink to="/" class="shrink-0 flex items-center gap-2.5">
            <img
              v-if="shop.config.site_logo"
              :src="shop.config.site_logo"
              :alt="shop.config.site_name"
              class="max-h-9 w-auto"
            />
            <template v-else>
              <img src="/icon-192.png" alt="" class="w-9 h-9 rounded-xl shrink-0" />
              <span class="text-base font-semibold text-gray-800 truncate max-w-[40vw] sm:max-w-none">
                {{ shop.config.site_name }}
              </span>
            </template>
          </RouterLink>

          <form class="flex-1 max-w-sm hidden sm:block" @submit.prevent="search">
            <div class="relative">
              <Search class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-300" />
              <input
                v-model="keyword"
                type="search"
                placeholder="搜索商品"
                aria-label="搜索商品"
                @input="onType"
                class="w-full pl-9 pr-3.5 py-2 bg-[#faf8f5] rounded-xl text-sm text-gray-800 placeholder:text-gray-500 focus:outline-none focus:ring-2 focus:ring-[#4a9d9a]/30 transition-all duration-200"
              />
            </div>
          </form>

          <nav class="ml-auto hidden md:flex items-center gap-1">
            <RouterLink
              to="/"
              class="px-3.5 py-2 rounded-xl text-sm font-medium text-gray-500 hover:text-[#4a9d9a] hover:bg-[#faf8f5] transition-all duration-200"
            >
              首页
            </RouterLink>
            <RouterLink
              to="/order"
              class="px-3.5 py-2 rounded-xl text-sm font-medium text-gray-500 hover:text-[#4a9d9a] hover:bg-[#faf8f5] transition-all duration-200"
            >
              订单查询
            </RouterLink>
            <ContactMenu placement="down" class="ml-1" />
          </nav>

          <button
            class="ml-auto md:hidden p-2 -mr-2 rounded-xl text-gray-500 hover:bg-[#faf8f5] transition-all duration-200"
            :aria-expanded="menuOpen"
            aria-label="菜单"
            @click="menuOpen = !menuOpen"
          >
            <component :is="menuOpen ? X : Menu" class="w-5 h-5" />
          </button>
        </div>
      </div>

      <Transition
        enter-active-class="transition-all duration-200"
        enter-from-class="opacity-0 -translate-y-2"
        leave-active-class="transition-all duration-200"
        leave-to-class="opacity-0 -translate-y-2"
      >
        <nav v-if="menuOpen" class="md:hidden border-t border-gray-100 bg-white">
          <div class="max-w-6xl mx-auto px-5 py-3 space-y-1">
            <form class="pb-2 sm:hidden" @submit.prevent="search">
              <div class="relative">
                <Search class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-300" />
                <input
                  v-model="keyword"
                  type="search"
                  placeholder="搜索商品"
                  aria-label="搜索商品"
                  @input="onType"
                  class="w-full pl-9 pr-3.5 py-2 bg-[#faf8f5] rounded-xl text-sm text-gray-800 placeholder:text-gray-500 focus:outline-none focus:ring-2 focus:ring-[#4a9d9a]/30"
                />
              </div>
            </form>
            <RouterLink
              to="/"
              class="block px-3.5 py-2.5 rounded-xl text-sm font-medium text-gray-600 hover:bg-[#faf8f5] transition-colors duration-200"
              @click="menuOpen = false"
            >
              首页
            </RouterLink>
            <RouterLink
              to="/order"
              class="block px-3.5 py-2.5 rounded-xl text-sm font-medium text-gray-600 hover:bg-[#faf8f5] transition-colors duration-200"
              @click="menuOpen = false"
            >
              订单查询
            </RouterLink>
            <!-- 移动端直接平铺：在已经展开的菜单里再套一层弹层很难点 -->
            <button
              v-for="(c, i) in contacts"
              :key="`${c.type}-${i}`"
              type="button"
              class="w-full flex items-center gap-2.5 px-3.5 py-2.5 rounded-xl text-sm font-medium text-gray-600 hover:bg-[#faf8f5] transition-colors duration-200"
              @click="pickAndClose(c)"
            >
              <component :is="iconOf(c)" class="w-4 h-4 shrink-0 text-[#4a9d9a]" />
              <span class="min-w-0 flex-1 text-left">
                <span class="block">{{ c.label }}</span>
                <span class="block text-xs text-gray-400 truncate">{{ c.value }}</span>
              </span>
              <component :is="actionIconOf(c)" class="w-3.5 h-3.5 shrink-0 text-gray-300" />
            </button>
          </div>
        </nav>
      </Transition>
    </header>

    <main class="flex-1">
      <RouterView v-slot="{ Component }">
        <component :is="Component" />
      </RouterView>
    </main>

    <footer class="mt-16 bg-white border-t border-gray-100">
      <div class="max-w-6xl mx-auto px-5 sm:px-6 lg:px-8 py-10">
        <div class="flex flex-col md:flex-row md:items-start md:justify-between gap-8">
          <div class="max-w-md">
            <p v-if="shop.config.site_footer" class="text-sm text-gray-500 leading-relaxed">
              {{ shop.config.site_footer }}
            </p>
            <p class="mt-3 text-xs text-gray-500 leading-relaxed">
              购买前请仔细阅读商品说明。虚拟商品一经发出不支持退换，如有疑问请先联系客服。
            </p>
          </div>

          <div class="flex flex-col gap-2.5 text-xs text-gray-500 md:text-right">
            <RouterLink
              to="/order"
              class="font-medium text-[#4a9d9a] hover:underline md:self-end"
            >
              订单查询
            </RouterLink>
            <div class="md:self-end">
              <ContactMenu />
            </div>
            <a
              v-if="shop.config.icp"
              href="https://beian.miit.gov.cn/"
              target="_blank"
              rel="noopener noreferrer"
              class="hover:text-gray-600 transition-colors duration-200"
            >
              {{ shop.config.icp }}
            </a>
            <a
              :href="PROJECT_URL"
              target="_blank"
              rel="noopener noreferrer"
              class="inline-flex items-center gap-1.5 hover:text-gray-600 transition-colors duration-200 md:self-end"
            >
              <Github class="w-3.5 h-3.5" />
              MoeCard
            </a>
            <span>© {{ new Date().getFullYear() }} {{ shop.config.site_name }}</span>
          </div>
        </div>
      </div>
    </footer>

    <!-- 公告弹窗。放在布局层，前台任何页面进来都会弹一次 -->
    <NoticeDialog />
  </div>
</template>
