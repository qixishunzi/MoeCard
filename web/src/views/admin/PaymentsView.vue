<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Copy, FlaskConical, Plus } from 'lucide-vue-next'
import { ApiError, adminApi } from '@/api'
import type { AdminPaymentChannel, PaymentProviderDesc } from '@/api/types'
import { copyText } from '@/utils/format'
import {
  WdBadge,
  WdButton,
  WdCard,
  WdInput,
  WdModal,
  WdSwitch,
  WdTable,
  confirmDialog,
  toast,
  type Column,
} from '@/ui'

const channels = ref<AdminPaymentChannel[]>([])
const providers = ref<PaymentProviderDesc[]>([])
const loading = ref(false)

const dialogVisible = ref(false)
const editing = ref<AdminPaymentChannel | null>(null)
const submitting = ref(false)

const form = reactive({
  name: '',
  provider: '',
  icon: '',
  status: 'disabled' as 'enabled' | 'disabled',
  sort: 0 as number | null,
  remark: '',
  config: {} as Record<string, string>,
})

const testing = ref(0)
const testResult = ref<Record<string, unknown> | null>(null)
const testVisible = ref(false)

const currentProvider = computed(() => providers.value.find((p) => p.key === form.provider))

/** provider → 品牌色，用于列表里那个小色块 */
const providerTint: Record<string, string> = {
  alipay: 'bg-[#1678ff]',
  wechat: 'bg-[#07c160]',
  stripe: 'bg-[#635bff]',
  hashpay: 'bg-[#f7931a]',
  yipay_v1: 'bg-[#4a9d9a]',
  yipay_v2: 'bg-[#6b8e8e]',
}

const columns: Column[] = [
  { key: 'sort', label: '排序', width: '70px', align: 'center' },
  { key: 'name', label: '渠道名称', width: '190px' },
  { key: 'notify_url', label: '异步回调地址' },
  { key: 'available', label: '可用性', width: '100px', align: 'center' },
  { key: 'status', label: '启用', width: '80px', align: 'center' },
  { key: 'actions', label: '操作', width: '170px', align: 'center' },
]

async function load() {
  loading.value = true
  try {
    const [chs, ps] = await Promise.all([adminApi.paymentChannels(), adminApi.paymentProviders()])
    channels.value = chs
    providers.value = ps
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
}

function providerName(key: string) {
  return providers.value.find((p) => p.key === key)?.name ?? key
}

/** 切换 provider 时用字段默认值填充配置 */
function applyDefaults() {
  const p = currentProvider.value
  if (!p) return
  const next: Record<string, string> = {}
  for (const f of p.fields) next[f.key] = form.config[f.key] ?? f.default ?? ''
  form.config = next
}

function openCreate() {
  editing.value = null
  Object.assign(form, {
    name: '',
    provider: providers.value[0]?.key ?? '',
    icon: '',
    status: 'disabled',
    sort: 0,
    remark: '',
    config: {},
  })
  applyDefaults()
  if (!form.name) form.name = currentProvider.value?.name ?? ''
  dialogVisible.value = true
}

function openEdit(row: AdminPaymentChannel) {
  editing.value = row
  Object.assign(form, {
    name: row.name,
    provider: row.provider,
    icon: row.icon,
    status: row.status,
    sort: row.sort,
    remark: row.remark,
    // 后端返回的敏感字段是脱敏值（如 sk_l********）。
    // 原样提交回去，后端会识别为「未修改」并保留旧值。
    config: { ...row.config },
  })
  dialogVisible.value = true
}

function onProviderChange() {
  form.config = {}
  applyDefaults()
  if (!form.name) form.name = currentProvider.value?.name ?? ''
}

async function submit() {
  if (!form.name.trim()) {
    toast.error('请输入渠道名称')
    return
  }
  if (!form.provider) {
    toast.error('请选择渠道类型')
    return
  }

  submitting.value = true
  try {
    const payload = {
      name: form.name.trim(),
      provider: form.provider,
      icon: form.icon,
      status: form.status,
      sort: form.sort ?? 0,
      remark: form.remark,
      config: form.config,
    }
    if (editing.value) {
      await adminApi.updateChannel(editing.value.id, payload)
      toast.success('支付渠道已更新')
    } else {
      await adminApi.createChannel(payload)
      toast.success('支付渠道已创建')
    }
    dialogVisible.value = false
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    submitting.value = false
  }
}

async function toggleStatus(row: AdminPaymentChannel) {
  const next = row.status === 'enabled' ? 'disabled' : 'enabled'
  try {
    // config 原样回传（含脱敏值），后端会保留原始密钥
    await adminApi.updateChannel(row.id, {
      name: row.name,
      provider: row.provider,
      icon: row.icon,
      sort: row.sort,
      remark: row.remark,
      config: row.config,
      status: next,
    })
    row.status = next
    toast.success(next === 'enabled' ? '已启用' : '已禁用')
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '操作失败')
    await load()
  }
}

async function remove(row: AdminPaymentChannel) {
  const ok = await confirmDialog({
    title: '删除支付渠道',
    message: `确定删除支付渠道「${row.name}」吗？`,
    confirmText: '删除',
    tone: 'danger',
  })
  if (!ok) return
  try {
    await adminApi.deleteChannel(row.id)
    toast.success('已删除')
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '删除失败')
  }
}

async function test(row: AdminPaymentChannel) {
  testing.value = row.id
  try {
    testResult.value = await adminApi.testChannel(row.id)
    testVisible.value = true
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '测试失败')
  } finally {
    testing.value = 0
  }
}

async function copyNotify(url: string) {
  const ok = await copyText(url)
  ok ? toast.success('回调地址已复制') : toast.error('复制失败')
}

const enabledCount = computed(() => channels.value.filter((c) => c.status === 'enabled').length)

onMounted(load)
</script>

<template>
  <div class="space-y-5">
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
      <p class="text-xs text-gray-500">
        共 <span class="tabular text-gray-700">{{ channels.length }}</span> 个渠道，
        <span class="tabular text-gray-700">{{ enabledCount }}</span> 个已启用
        <span v-if="channels.length && !enabledCount" class="text-[#c17767]">
          —— 一个都没启用，前台无法下单
        </span>
      </p>
      <WdButton variant="primary" @click="openCreate">
        <Plus class="w-4 h-4" />
        添加支付渠道
      </WdButton>
    </div>

    <div class="px-5 py-4 rounded-2xl bg-white shadow-xl shadow-black/[0.04]">
      <p class="text-sm font-medium text-gray-800">配置支付渠道前请注意</p>
      <ol class="mt-2 space-y-1 text-xs text-gray-500 leading-relaxed list-decimal list-inside">
        <li>每个渠道有独立的<span class="font-medium text-gray-700">异步回调地址</span>，必须填到对应支付平台的商户后台</li>
        <li>密钥类配置保存后只显示脱敏值，编辑时留着不动即可保留原值</li>
        <li>同一种支付类型可以创建多个渠道（如「易支付-主线」「易支付-备线」）</li>
        <li>保存后建议点「测试」验证配置 —— 会创建一笔 1 分钱的测试单，不会真实扣款</li>
      </ol>
    </div>

    <WdCard flush>
      <div class="px-6 py-5">
        <WdTable :columns="columns" :rows="channels" :loading="loading">
          <template #sort="{ row }">
            <span class="tabular text-gray-400">{{ row.sort }}</span>
          </template>

          <template #name="{ row }">
            <div class="flex items-center gap-3">
              <img
                v-if="row.icon"
                :src="row.icon"
                :alt="row.name"
                class="w-8 h-8 rounded-lg object-contain shrink-0"
              />
              <span
                v-else
                class="w-8 h-8 rounded-lg shrink-0"
                :class="providerTint[row.provider] ?? 'bg-[#6b8e8e]'"
              />
              <div class="min-w-0">
                <p class="text-sm font-medium text-gray-800 truncate">{{ row.name }}</p>
                <p class="text-xs text-gray-400 truncate">{{ providerName(row.provider) }}</p>
              </div>
            </div>
          </template>

          <template #notify_url="{ row }">
            <div class="flex items-center gap-2 min-w-0">
              <span class="font-mono text-[11px] text-gray-500 truncate">{{ row.notify_url }}</span>
              <button
                class="shrink-0 p-1 rounded-lg text-gray-300 hover:text-[#4a9d9a] hover:bg-[#faf8f5] transition-all duration-200"
                aria-label="复制回调地址"
                @click="copyNotify(row.notify_url)"
              >
                <Copy class="w-3.5 h-3.5" />
              </button>
            </div>
          </template>

          <template #available="{ row }">
            <WdBadge :tone="row.available ? 'teal' : 'clay'">
              {{ row.available ? '正常' : '不可用' }}
            </WdBadge>
          </template>

          <template #status="{ row }">
            <WdSwitch
              :model-value="row.status === 'enabled'"
              :disabled="!row.available"
              :label="`${row.name} 启用状态`"
              @update:model-value="toggleStatus(row as AdminPaymentChannel)"
            />
          </template>

          <template #actions="{ row }">
            <div class="flex items-center justify-center gap-3">
              <button
                class="text-xs font-medium text-[#4a9d9a] hover:underline"
                @click="openEdit(row as AdminPaymentChannel)"
              >
                配置
              </button>
              <button
                class="text-xs font-medium text-[#8f7243] hover:underline disabled:opacity-50"
                :disabled="testing === row.id"
                @click="test(row as AdminPaymentChannel)"
              >
                {{ testing === row.id ? '测试中' : '测试' }}
              </button>
              <button
                class="text-xs font-medium text-[#c17767] hover:underline"
                @click="remove(row as AdminPaymentChannel)"
              >
                删除
              </button>
            </div>
          </template>

          <template #empty>
            <span>还没有配置支付渠道，用户将无法下单支付</span>
          </template>
        </WdTable>
      </div>
    </WdCard>

    <!-- 配置 -->
    <WdModal
      v-model="dialogVisible"
      :title="editing ? `配置 ${form.name}` : '添加支付渠道'"
      width="640px"
      :close-on-overlay="false"
    >
      <div class="space-y-4">
        <div>
          <WdInput
            v-model="form.provider"
            type="select"
            label="渠道类型"
            required
            :disabled="!!editing"
            :options="providers.map((p) => ({ label: p.name, value: p.key }))"
            @change="onProviderChange"
          />
          <p
            v-if="currentProvider?.note"
            class="mt-2 px-3.5 py-2.5 rounded-xl bg-[#faf8f5] text-xs text-gray-500 leading-relaxed"
          >
            {{ currentProvider.note }}
          </p>
        </div>

        <WdInput
          v-model="form.name"
          label="渠道名称"
          required
          :maxlength="30"
          placeholder="展示给用户的名称，如「支付宝」「Stripe US」"
        />

        <WdInput v-model="form.icon" label="图标 URL" placeholder="可选，留空则使用默认色块" />

        <div class="flex items-center gap-3 pt-2">
          <span class="h-px flex-1 bg-gray-100" />
          <span class="text-xs text-gray-400">渠道配置</span>
          <span class="h-px flex-1 bg-gray-100" />
        </div>

        <!--
          配置表单由后端的 ConfigSchema 驱动 —— 新增支付渠道时这里不用改任何代码。
          switch 类型走开关组件，其余交给 WdInput。
        -->
        <div v-for="f in currentProvider?.fields ?? []" :key="f.key">
          <template v-if="f.type === 'switch'">
            <label class="block mb-1.5 text-xs font-medium text-gray-500">{{ f.label }}</label>
            <div class="h-[42px] flex items-center gap-3">
              <WdSwitch
                :model-value="form.config[f.key] === '1'"
                :label="f.label"
                @update:model-value="(v) => (form.config[f.key] = v ? '1' : '0')"
              />
              <span class="text-sm text-gray-500">
                {{ form.config[f.key] === '1' ? '已开启' : '已关闭' }}
              </span>
            </div>
          </template>

          <WdInput
            v-else
            v-model="form.config[f.key]"
            :type="f.type as 'text' | 'password' | 'textarea' | 'select' | 'number'"
            :label="f.label"
            :required="f.required"
            :placeholder="f.placeholder"
            :options="f.options?.map((o) => ({ label: o.label, value: o.value }))"
            :rows="4"
            :mono="f.type === 'textarea'"
          />

          <p v-if="f.help" class="mt-1.5 text-xs text-gray-400 leading-relaxed">{{ f.help }}</p>
          <p v-if="f.secret && editing" class="mt-1 text-xs text-[#b8873f] leading-relaxed">
            敏感字段：当前显示的是脱敏值，不修改则保留原值
          </p>
        </div>

        <div class="flex items-center gap-3 pt-2">
          <span class="h-px flex-1 bg-gray-100" />
        </div>

        <div class="grid sm:grid-cols-2 gap-4">
          <WdInput
            v-model="form.sort"
            type="number"
            label="排序"
            :min="0"
            hint="数值越大越靠前"
          />
          <div>
            <label class="block mb-1.5 text-xs font-medium text-gray-500">状态</label>
            <div class="h-[42px] flex items-center gap-3">
              <WdSwitch
                :model-value="form.status === 'enabled'"
                label="启用渠道"
                @update:model-value="(v) => (form.status = v ? 'enabled' : 'disabled')"
              />
              <span class="text-sm text-gray-500">
                {{ form.status === 'enabled' ? '启用' : '禁用' }}
              </span>
            </div>
          </div>
        </div>

        <WdInput v-model="form.remark" type="textarea" label="备注" :rows="2" :maxlength="200" />

        <div v-if="editing">
          <label class="block mb-1.5 text-xs font-medium text-gray-500">异步回调地址</label>
          <div class="flex items-center gap-2 px-3.5 py-2.5 rounded-xl bg-[#faf8f5]">
            <span class="flex-1 font-mono text-xs text-gray-600 break-all">
              {{ editing.notify_url }}
            </span>
            <button
              class="shrink-0 text-xs font-medium text-[#4a9d9a] hover:underline"
              @click="copyNotify(editing.notify_url)"
            >
              复制
            </button>
          </div>
          <p class="mt-1.5 text-xs text-gray-400">
            请把这个地址填到支付平台的「异步通知地址 / Webhook」配置中。
          </p>
        </div>
      </div>

      <template #footer>
        <WdButton @click="dialogVisible = false">取消</WdButton>
        <WdButton variant="primary" :loading="submitting" @click="submit">保存</WdButton>
      </template>
    </WdModal>

    <!-- 测试结果 -->
    <WdModal v-model="testVisible" title="配置测试结果" width="560px">
      <div class="px-4 py-3 rounded-xl bg-[#4a9d9a]/10 text-sm text-[#4a9d9a] flex items-start gap-2.5">
        <FlaskConical class="w-4 h-4 mt-0.5 shrink-0" />
        <span>{{ testResult?.message }}</span>
      </div>

      <dl class="mt-4 space-y-3 text-sm">
        <div class="flex gap-4">
          <dt class="w-20 shrink-0 text-gray-400">返回类型</dt>
          <dd class="text-gray-700">{{ testResult?.action }}</dd>
        </div>
        <div v-if="testResult?.url" class="flex gap-4">
          <dt class="w-20 shrink-0 text-gray-400">支付地址</dt>
          <dd class="font-mono text-xs text-gray-600 break-all">{{ testResult.url }}</dd>
        </div>
        <div v-if="testResult?.qrcode" class="flex gap-4">
          <dt class="w-20 shrink-0 text-gray-400">二维码内容</dt>
          <dd class="font-mono text-xs text-gray-600 break-all">{{ testResult.qrcode }}</dd>
        </div>
        <div class="flex gap-4">
          <dt class="w-20 shrink-0 text-gray-400">回调地址</dt>
          <dd class="font-mono text-xs text-gray-600 break-all">{{ testResult?.notify_url }}</dd>
        </div>
      </dl>

      <p class="mt-4 px-3.5 py-2.5 rounded-xl bg-[#faf8f5] text-xs text-gray-500 leading-relaxed">
        测试只验证「能否成功创建支付单」，也就是网关地址、商户号、密钥、签名算法都正确。
        回调验签是否正常，需要用真实小额订单跑通一次完整流程。
      </p>
    </WdModal>
  </div>
</template>
