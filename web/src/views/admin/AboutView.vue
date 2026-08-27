<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Github, Send, Users } from 'lucide-vue-next'
import { adminApi } from '@/api'
import type { BuildInfo, UpdateCheck } from '@/api/types'
import {
  PROJECT_AUTHOR,
  PROJECT_NAME,
  PROJECT_TG_CHANNEL,
  PROJECT_TG_GROUP,
  PROJECT_URL,
} from '@/constants'
import { WdButton, WdCard, toast } from '@/ui'

/**
 * 版本号从后端拿，而不是写死在前端。
 *
 * 它是编译时用 -ldflags 注进二进制的，前端根本不知道自己被打进了哪一版；
 * 写死的话改了版本忘了同步，显示的就是个假数字。
 */
const build = ref<BuildInfo | null>(null)

onMounted(async () => {
  try {
    build.value = await adminApi.build()
  } catch {
    // 拿不到就不显示这块，不值得为它弹个错误
    build.value = null
  }
})

// ---- 检查更新 ----
//
// 只查不装。装的动作会换掉磁盘上的可执行文件并且必须重启进程，
// 让网页去重启自己所在的进程，出问题时既难排查又容易把站点弄挂 ——
// 所以这里只负责告诉你有新版本和那一行命令。
const update = ref<UpdateCheck | null>(null)
const checking = ref(false)

async function checkUpdate() {
  checking.value = true
  try {
    update.value = await adminApi.checkUpdate()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '检查失败')
  } finally {
    checking.value = false
  }
}

const updateCmd = 'moecard -update'

const LINKS = [
  {
    icon: Github,
    label: 'GitHub 项目',
    value: PROJECT_URL,
    href: PROJECT_URL,
    hint: '源码、更新日志、问题反馈',
  },
  {
    icon: Send,
    label: 'Telegram 频道',
    value: PROJECT_TG_CHANNEL,
    href: PROJECT_TG_CHANNEL,
    hint: '版本发布与重要通知',
  },
  {
    icon: Users,
    label: 'Telegram 群组',
    value: PROJECT_TG_GROUP,
    href: PROJECT_TG_GROUP,
    hint: '使用交流与互助',
  },
]

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast.success('已复制')
  } catch {
    toast.error('复制失败，请手动选中复制')
  }
}
</script>

<template>
  <div class="space-y-5 max-w-3xl">
    <WdCard>
      <div class="flex flex-col sm:flex-row sm:items-center gap-5">
        <img src="/icon-192.png" alt="MoeCard" class="w-16 h-16 rounded-2xl shrink-0" />
        <div class="min-w-0">
          <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
            <h2 class="text-xl font-semibold text-gray-800">{{ PROJECT_NAME }}</h2>
            <span v-if="build" class="text-sm font-mono text-gray-400">
              v{{ build.version }}
            </span>
          </div>
          <p class="mt-1.5 text-sm text-gray-500 leading-relaxed">
            单文件部署的数字商品自动发货商城。作者
            <span class="text-gray-700 font-medium">{{ PROJECT_AUTHOR }}</span>
          </p>
        </div>
      </div>
    </WdCard>

    <WdCard title="版本更新" subtitle="有新版本时这里会告诉你怎么升">
      <div class="space-y-4">
        <div class="flex flex-wrap items-center gap-3">
          <WdButton :loading="checking" @click="checkUpdate">检查更新</WdButton>
          <span v-if="!update" class="text-xs text-gray-400">
            点一下会连到 GitHub 查询最新版本
          </span>
        </div>

        <!-- 连不上 GitHub 不算错误：内网部署本来就出不去 -->
        <p
          v-if="update?.error"
          class="px-4 py-3.5 rounded-xl bg-[#faf8f5] text-xs text-gray-500 leading-relaxed"
        >
          查询失败：{{ update.error }}<br />
          内网部署连不上 GitHub 属于正常情况，可以到发布页手动看有没有新版本。
        </p>

        <template v-else-if="update">
          <p
            v-if="!update.has_update"
            class="px-4 py-3.5 rounded-xl bg-[#4a9d9a]/10 text-sm text-[#3b7d7a]"
          >
            已经是最新版本（{{ update.latest }}）。
          </p>

          <div v-else class="space-y-3">
            <div class="px-4 py-3.5 rounded-xl bg-[#e8b86d]/15 text-[#8f7243]">
              <p class="text-sm font-medium">
                有新版本 {{ update.latest }}（当前 {{ update.current }}）
              </p>
              <p
                v-if="update.release?.notes"
                class="mt-2 text-xs leading-relaxed whitespace-pre-line max-h-40 overflow-y-auto"
              >
                {{ update.release.notes }}
              </p>
              <a
                v-if="update.release?.url"
                :href="update.release.url"
                target="_blank"
                rel="noopener noreferrer"
                class="mt-2 inline-block text-xs font-medium underline"
              >
                查看发布页
              </a>
            </div>

            <div v-if="update.supported" class="space-y-2">
              <p class="text-xs text-gray-500 leading-relaxed">
                在服务器上执行这行命令升级。会自动校验下载完整性，
                旧版本备份成 <span class="font-mono">moecard.old</span>，
                更新完需要重启服务。
              </p>
              <div class="flex items-center gap-2">
                <code
                  class="flex-1 min-w-0 px-3.5 py-2.5 rounded-xl bg-[#faf8f5] text-sm font-mono text-gray-700 break-all"
                >
                  {{ updateCmd }}
                </code>
                <WdButton @click="copy(updateCmd)">复制</WdButton>
              </div>
            </div>
            <p
              v-else
              class="px-4 py-3.5 rounded-xl bg-[#faf8f5] text-xs text-gray-500 leading-relaxed"
            >
              {{ update.reason }}
            </p>
          </div>
        </template>
      </div>
    </WdCard>

    <WdCard title="项目与社区" subtitle="源码、更新与交流">
      <div class="space-y-3">
        <div
          v-for="l in LINKS"
          :key="l.href"
          class="flex flex-col sm:flex-row sm:items-center gap-3 py-3 border-b border-gray-100 last:border-0 last:pb-0"
        >
          <span class="w-9 h-9 rounded-xl bg-[#4a9d9a]/10 grid place-items-center shrink-0">
            <component :is="l.icon" class="w-4 h-4 text-[#4a9d9a]" />
          </span>
          <div class="min-w-0 flex-1">
            <p class="text-sm font-medium text-gray-800">{{ l.label }}</p>
            <a
              :href="l.href"
              target="_blank"
              rel="noopener noreferrer"
              class="text-xs font-mono text-[#4a9d9a] hover:underline break-all"
            >
              {{ l.value }}
            </a>
            <p class="mt-0.5 text-xs text-gray-400">{{ l.hint }}</p>
          </div>
          <div class="flex gap-2 shrink-0">
            <WdButton @click="copy(l.href)">复制</WdButton>
            <!--
              后台是内网/本机常见的使用场景，浏览器不一定能直接打开外链，
              所以复制按钮和打开按钮都留着。
            -->
            <a :href="l.href" target="_blank" rel="noopener noreferrer">
              <WdButton variant="primary">打开</WdButton>
            </a>
          </div>
        </div>
      </div>
    </WdCard>
  </div>
</template>
