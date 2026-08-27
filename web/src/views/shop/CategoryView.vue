<script setup lang="ts">
import { onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Loader2 } from 'lucide-vue-next'
import { useShopStore } from '@/stores/shop'

/**
 * /category/:slug 是对外的友好链接（利于分享与 SEO），
 * 内部统一收敛到首页的分类筛选，避免维护两套列表逻辑。
 */
const route = useRoute()
const router = useRouter()
const shop = useShopStore()

onMounted(async () => {
  if (!shop.loaded) await shop.load().catch(() => undefined)
  const slug = String(route.params.slug || '')
  const cat = shop.categories.find((c) => c.slug === slug)
  router.replace({
    name: 'home',
    query: cat ? { category_id: String(cat.id) } : {},
  })
})
</script>

<template>
  <div class="py-32 flex justify-center">
    <Loader2 class="w-7 h-7 text-[#4a9d9a] animate-spin" />
  </div>
</template>
