<script setup lang="ts">
import { useRouter } from 'vue-router'
import { Compass } from 'lucide-vue-next'
import { useShopStore } from '@/stores/shop'
import { WdButton, WdCard } from '@/ui'

const shop = useShopStore()
const router = useRouter()
</script>

<template>
  <div class="min-h-screen bg-[#faf8f5] flex items-center justify-center px-5 py-12">
    <WdCard class="w-full max-w-md">
      <div class="flex flex-col items-center text-center py-4">
        <span class="w-16 h-16 rounded-2xl bg-[#faf8f5] grid place-items-center">
          <Compass class="w-8 h-8 text-gray-300" />
        </span>

        <p class="mt-5 text-4xl font-semibold text-gray-500 tabular">404</p>
        <h1 class="mt-3 text-lg font-semibold text-gray-800">页面不存在</h1>
        <p class="mt-2 text-sm text-gray-500 leading-relaxed">
          您访问的页面可能已被移除、更名，或暂时不可用。
        </p>

        <div class="mt-6 flex flex-col sm:flex-row gap-3 w-full">
          <WdButton variant="primary" block @click="router.push('/')">
            返回 {{ shop.config.site_name }}
          </WdButton>
          <WdButton block @click="router.push('/order')">查询订单</WdButton>
        </div>
      </div>
    </WdCard>
  </div>
</template>
