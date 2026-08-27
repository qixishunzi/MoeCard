<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { AlertTriangle, BellRing, Save, Send } from 'lucide-vue-next'
import { ApiError, adminApi } from '@/api'
import type { NotifyProviderDesc } from '@/api/types'
import { WdBadge, WdButton, WdCard, WdInput, WdSwitch, toast } from '@/ui'

const providers = ref<NotifyProviderDesc[]>([])
const loading = ref(true)
const saving = ref(false)
const testing = ref('')

/** 总开关与事件开关 */
const form = reactive({
  notify_enabled: '0',
  notify_on_paid: '0',
  notify_on_manual: '1',
  notify_on_attention: '1',
  notify_on_low_stock: '1',
  notify_on_refund: '1',
  low_stock_threshold: '5',
})

/** 各渠道是否启用 */
const enabled = reactive<Record<string, boolean>>({})
/** 各渠道的配置值：cfg[渠道key][字段key] */
const cfg = reactive<Record<string, Record<string, string>>>({})

const EVENTS = [
  {
    key: 'notify_on_manual' as const,
    title: '待人工发货',
    desc: '手动发货商品付款后立即提醒 —— 有人正在等你操作',
    recommended: true,
  },
  {
    key: 'notify_on_attention' as const,
    title: '订单需要处理',
    desc: '买家已付款但系统发不出货（缺货、商品被删等），必须尽快人工介入',
    recommended: true,
  },
  {
    key: 'notify_on_low_stock' as const,
    title: '库存告急',
    desc: '卡密低于阈值时提醒。卖光了不知道，订单就白白流失了',
    recommended: true,
  },
  { key: 'notify_on_refund' as const, title: '订单退款', desc: '发生退款时提醒', recommended: false },
  {
    key: 'notify_on_paid' as const,
    title: '每笔订单支付成功',
    desc: '自动发货商品也推。单量大时会很吵，建议保持关闭',
    recommended: false,
  },
]

const activeCount = computed(() => Object.values(enabled).filter(Boolean).length)

/**
 * 邮件渠道复用商城的 SMTP，没配好就发不出去 —— 提前提示，别等到测试时才报错。
 * 值在 load() 里按已保存的 SMTP 配置填。忘了填的话这条提示会一直挂着，
 * SMTP 明明是好的却天天警告，最后就没人看提示了。
 */
const smtpReady = ref(false)
const emailNeedsSMTP = computed(() => enabled.email === true && !smtpReady.value)
const isOn = computed(() => form.notify_enabled === '1')

async function load() {
  loading.value = true
  try {
    const [provs, settings] = await Promise.all([adminApi.notifyProviders(), adminApi.settings()])
    providers.value = provs

    for (const k of Object.keys(form) as (keyof typeof form)[]) {
      if (settings[k] !== undefined) form[k] = settings[k]
    }

    // 判定「SMTP 能用」：开关打开 + 有服务器地址 + 有发件人。
    // 三者缺一，邮件通知都发不出去。
    smtpReady.value =
      settings.smtp_enabled === '1' &&
      !!(settings.smtp_host || '').trim() &&
      !!(settings.smtp_from_email || '').trim()

    const active = (settings.notify_channels || '').split(',').map((s) => s.trim())
    for (const p of provs) {
      enabled[p.key] = active.includes(p.key)
      cfg[p.key] = {}
      for (const f of p.fields) {
        // 敏感字段这里拿到的是脱敏值，原样回传即表示"不修改"
        cfg[p.key][f.key] = settings[`notify_cfg_${p.key}_${f.key}`] ?? ''
      }
    }
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
}

function buildPayload(): Record<string, string> {
  const payload: Record<string, string> = { ...form }
  payload.notify_channels = providers.value
    .filter((p) => enabled[p.key])
    .map((p) => p.key)
    .join(',')
  for (const p of providers.value) {
    for (const f of p.fields) {
      payload[`notify_cfg_${p.key}_${f.key}`] = cfg[p.key]?.[f.key] ?? ''
    }
  }
  return payload
}

async function save() {
  const n = Number(form.low_stock_threshold)
  if (!Number.isInteger(n) || n < 0 || n > 100000) {
    toast.error('库存告警阈值必须是 0-100000 的整数')
    return
  }
  // 开了总开关却一个渠道都没选，等于没开 —— 提前拦住这个常见误配
  if (isOn.value && activeCount.value === 0) {
    toast.error('请至少启用一个通知渠道')
    return
  }

  saving.value = true
  try {
    await adminApi.updateSettings(buildPayload())
    toast.success('通知配置已保存')
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    saving.value = false
  }
}

/** 用当前表单里的值直接测试，不需要先保存 */
async function test(key: string) {
  testing.value = key
  try {
    await adminApi.testNotify(key, { ...cfg[key] })
    toast.success('测试通知已发送，请查收')
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '发送失败')
  } finally {
    testing.value = ''
  }
}

onMounted(load)
</script>

<template>
  <div class="space-y-5">
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
      <p class="text-xs text-gray-500">
        已启用 <span class="tabular text-gray-700">{{ activeCount }}</span> 个渠道
      </p>
      <WdButton variant="primary" :loading="saving" :disabled="loading" @click="save">
        <Save class="w-4 h-4" />
        保存配置
      </WdButton>
    </div>

    <!-- 总开关 -->
    <WdCard>
      <div class="flex items-start justify-between gap-6">
        <div class="min-w-0">
          <div class="flex items-center gap-2">
            <BellRing class="w-4 h-4 text-[#4a9d9a]" />
            <p class="text-sm font-medium text-gray-800">启用商家通知</p>
            <WdBadge v-if="isOn && activeCount === 0" tone="amber">未选择渠道</WdBadge>
          </div>
          <p class="mt-1.5 text-xs text-gray-500 leading-relaxed">
            开启后，需要你立刻处理的事（待发货订单、发不出货的订单、库存告急）会直接推到手机上，
            不用一直守着后台刷新。通知失败不会影响订单和发货。
          </p>
        </div>
        <WdSwitch
          :model-value="isOn"
          label="启用商家通知"
          @update:model-value="(v) => (form.notify_enabled = v ? '1' : '0')"
        />
      </div>
    </WdCard>

    <!-- 渠道 -->
    <WdCard
      v-for="p in providers"
      :key="p.key"
      :title="p.name"
      :subtitle="p.note"
    >
      <template #actions>
        <div class="flex items-center gap-2.5">
          <WdButton
            size="sm"
            :loading="testing === p.key"
            :disabled="!enabled[p.key]"
            @click="test(p.key)"
          >
            <Send class="w-3.5 h-3.5" />
            测试
          </WdButton>
          <WdSwitch
            :model-value="enabled[p.key] === true"
            :label="`启用 ${p.name}`"
            @update:model-value="(v) => (enabled[p.key] = v)"
          />
        </div>
      </template>

      <div
        v-if="p.key === 'email' && emailNeedsSMTP"
        class="mb-4 flex items-start gap-2.5 px-4 py-3.5 rounded-xl bg-[#e8b86d]/15 text-[#8f7243]"
      >
        <AlertTriangle class="w-4 h-4 mt-0.5 shrink-0" />
        <p class="text-sm leading-relaxed">
          邮件渠道用的是商城的 SMTP，但「邮件配置」里还没配好或没启用 ——
          现在保存的话通知会一直发送失败。
          <RouterLink :to="{ name: 'admin-mail' }" class="font-medium underline">
            去配置
          </RouterLink>
        </p>
      </div>

      <div v-if="enabled[p.key]" class="grid md:grid-cols-2 gap-4">
        <div v-for="f in p.fields" :key="f.key">
          <WdInput
            v-model="cfg[p.key][f.key]"
            :type="f.type === 'password' ? 'password' : 'text'"
            :label="f.label"
            :required="f.required"
            :placeholder="f.placeholder"
            :hint="f.help"
          />
          <p v-if="f.secret && cfg[p.key][f.key]?.includes('*')" class="mt-1 text-xs text-[#8f7243]">
            当前显示的是脱敏值，不修改则保留原值
          </p>
        </div>
      </div>
      <p v-else class="text-sm text-gray-500">未启用。打开右上角开关后可填写配置。</p>
    </WdCard>

    <!-- 事件开关 -->
    <WdCard title="推送哪些事件" subtitle="按需开关，避免被无关消息淹没">
      <div class="space-y-4">
        <div
          v-for="(e, i) in EVENTS"
          :key="e.key"
          class="flex items-start justify-between gap-6"
          :class="i < EVENTS.length - 1 && 'pb-4 border-b border-gray-100'"
        >
          <div class="min-w-0">
            <div class="flex items-center gap-2">
              <p class="text-sm font-medium text-gray-800">{{ e.title }}</p>
              <WdBadge v-if="e.recommended" tone="teal">建议开启</WdBadge>
            </div>
            <p class="mt-1 text-xs text-gray-500 leading-relaxed">{{ e.desc }}</p>
          </div>
          <WdSwitch
            :model-value="form[e.key] === '1'"
            :label="e.title"
            @update:model-value="(v) => (form[e.key] = v ? '1' : '0')"
          />
        </div>
      </div>
    </WdCard>

    <!-- 库存阈值 -->
    <WdCard title="库存告警阈值" subtitle="可用库存低于或等于该值时提醒，商品可单独覆盖">
      <div class="max-w-xs">
        <WdInput
          v-model="form.low_stock_threshold"
          type="number"
          label="全局默认阈值"
          :min="0"
          hint="设为 0 表示默认不告警"
        />
      </div>
      <p class="mt-4 px-3.5 py-2.5 rounded-xl bg-[#faf8f5] text-xs text-gray-500 leading-relaxed">
        每 15 分钟扫描一次。同一商品跌破阈值只提醒一次，补货回升后才会重置 ——
        否则每 15 分钟轰炸一遍，最后一定会被你关掉。
      </p>
    </WdCard>
  </div>
</template>
