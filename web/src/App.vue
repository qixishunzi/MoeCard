<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { Wrench } from 'lucide-vue-next'
import { useShopStore } from '@/stores/shop'
import { WdCard, WdToaster } from '@/ui'

const route = useRoute()
const shop = useShopStore()

// 维护模式只挡前台。后台与 /setup 必须始终可访问，
// 否则管理员会把自己锁在外面。
const showMaintenance = computed(
  () =>
    shop.config.maintenance &&
    !route.path.startsWith('/admin') &&
    route.name !== 'setup' &&
    !route.path.startsWith('/pay'),
)
</script>

<template>
  <div v-if="showMaintenance" class="min-h-screen bg-[#faf8f5] grid place-items-center px-5 py-12">
    <WdCard class="w-full max-w-md">
      <div class="flex flex-col items-center text-center py-4">
        <span class="w-16 h-16 rounded-2xl bg-[#e8b86d]/15 grid place-items-center">
          <Wrench class="w-8 h-8 text-[#e8b86d]" />
        </span>
        <h1 class="mt-5 text-xl font-semibold text-gray-800">{{ shop.config.site_name }}</h1>
        <p class="mt-3 text-sm text-gray-500 leading-relaxed">
          {{ shop.config.maintenance_text || '商城正在维护升级，请稍后再来。' }}
        </p>
        <RouterLink
          to="/order"
          class="mt-6 text-sm font-medium text-[#4a9d9a] hover:underline"
        >
          查询已有订单
        </RouterLink>
      </div>
    </WdCard>
  </div>

  <RouterView v-else />

  <!-- 全局消息与确认框。内部用 Teleport 挂到 body，不受此处位置影响 -->
  <WdToaster />
</template>
