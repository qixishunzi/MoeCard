<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Save, Send, Zap } from 'lucide-vue-next'
import { ApiError, adminApi } from '@/api'
import { WdBadge, WdButton, WdCard, WdInput, WdModal, WdSwitch, WdTabs, toast } from '@/ui'

const loading = ref(true)
const saving = ref(false)
const testing = ref(false)
const tab = ref('smtp')

const testVisible = ref(false)
const testTo = ref('')

const tabs = [
  { value: 'smtp', label: 'SMTP 服务器' },
  { value: 'templates', label: '邮件模板' },
]

const MAIL_KEYS = [
  'smtp_enabled',
  'smtp_host',
  'smtp_port',
  'smtp_username',
  'smtp_password',
  'smtp_from_email',
  'smtp_from_name',
  'smtp_encryption',
  'mail_notify_on_paid',
  'mail_notify_on_delivery',
  'mail_paid_subject',
  'mail_paid_body',
  'mail_deliver_subject',
  'mail_deliver_body',
  'mail_manual_subject',
  'mail_manual_body',
] as const

type MailKey = (typeof MAIL_KEYS)[number]

const form = reactive<Record<MailKey, string>>({
  smtp_enabled: '0',
  smtp_host: '',
  smtp_port: '465',
  smtp_username: '',
  smtp_password: '',
  smtp_from_email: '',
  smtp_from_name: '',
  smtp_encryption: 'ssl',
  mail_notify_on_paid: '1',
  mail_notify_on_delivery: '1',
  mail_paid_subject: '',
  mail_paid_body: '',
  mail_deliver_subject: '',
  mail_deliver_body: '',
  mail_manual_subject: '',
  mail_manual_body: '',
})

const errors = reactive<Partial<Record<MailKey, string>>>({})

/** 保存时拿到的原始密码掩码。用户没动过就原样回传，后端识别为「未修改」。 */
const passwordMask = ref('')

const ENCRYPTIONS = [
  { label: 'SSL / TLS（推荐，端口 465）', value: 'ssl' },
  { label: 'STARTTLS（端口 587）', value: 'starttls' },
  { label: '不加密（仅限内网自建）', value: 'none' },
]

/** 常见邮箱服务商的 SMTP 参数，省去用户翻文档。 */
const PRESETS = [
  {
    name: 'QQ 邮箱',
    host: 'smtp.qq.com',
    port: '465',
    encryption: 'ssl',
    note: '密码填「授权码」，不是登录密码',
  },
  {
    name: '163 邮箱',
    host: 'smtp.163.com',
    port: '465',
    encryption: 'ssl',
    note: '密码填「客户端授权密码」',
  },
  {
    name: 'Gmail',
    host: 'smtp.gmail.com',
    port: '587',
    encryption: 'starttls',
    note: '需开启两步验证并使用应用专用密码',
  },
  {
    name: 'Outlook',
    host: 'smtp.office365.com',
    port: '587',
    encryption: 'starttls',
    note: '',
  },
  {
    name: '阿里云企业邮',
    host: 'smtp.qiye.aliyun.com',
    port: '465',
    encryption: 'ssl',
    note: '',
  },
  {
    name: 'SendGrid',
    host: 'smtp.sendgrid.net',
    port: '587',
    encryption: 'starttls',
    note: '用户名固定为 apikey',
  },
  {
    name: 'Mailgun',
    host: 'smtp.mailgun.org',
    port: '587',
    encryption: 'starttls',
    note: '',
  },
  {
    name: 'Amazon SES',
    host: 'email-smtp.us-east-1.amazonaws.com',
    port: '587',
    encryption: 'starttls',
    note: '注意区域前缀',
  },
]

const activePreset = ref('')

/** 模板可用变量。所有模板共用前 6 个，发货类模板多一个卡密内容。 */
const COMMON_VARS = [
  { key: '{{site_name}}', desc: '商城名称' },
  { key: '{{order_no}}', desc: '订单号' },
  { key: '{{product_name}}', desc: '商品名称' },
  { key: '{{quantity}}', desc: '购买数量' },
  { key: '{{pay_amount}}', desc: '支付金额' },
  { key: '{{paid_at}}', desc: '支付时间' },
  { key: '{{order_url}}', desc: '订单查询链接' },
]
const DELIVERY_VARS = [{ key: '{{delivery_content}}', desc: '发货内容 / 卡密' }]

const templateGroups = computed(() => [
  {
    title: '支付成功通知',
    desc: '订单支付成功后立即发送。手动发货商品尤其需要 —— 让用户知道钱已经到账。',
    toggleKey: 'mail_notify_on_paid' as MailKey,
    toggleLabel: '支付成功后发送邮件',
    subjectKey: 'mail_paid_subject' as MailKey,
    bodyKey: 'mail_paid_body' as MailKey,
    vars: COMMON_VARS,
  },
  {
    title: '自动发货通知',
    desc: '卡密自动发放后发送，正文里带卡密内容。',
    toggleKey: 'mail_notify_on_delivery' as MailKey,
    toggleLabel: '发货后发送邮件',
    subjectKey: 'mail_deliver_subject' as MailKey,
    bodyKey: 'mail_deliver_body' as MailKey,
    vars: [...COMMON_VARS, ...DELIVERY_VARS],
  },
  {
    title: '手动发货通知',
    desc: '管理员在订单页手动发货时发送。不受上面的开关控制，发货动作本身就意味着要通知用户。',
    toggleKey: null,
    toggleLabel: '',
    subjectKey: 'mail_manual_subject' as MailKey,
    bodyKey: 'mail_manual_body' as MailKey,
    vars: [...COMMON_VARS, ...DELIVERY_VARS],
  },
])

const configured = computed(() => !!form.smtp_host.trim() && !!form.smtp_from_email.trim())

async function load() {
  loading.value = true
  try {
    const data = await adminApi.settings()
    for (const k of MAIL_KEYS) {
      if (data[k] !== undefined) form[k] = data[k]
    }
    passwordMask.value = form.smtp_password
    matchPreset()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '加载配置失败')
  } finally {
    loading.value = false
  }
}

function matchPreset() {
  const hit = PRESETS.find((p) => p.host === form.smtp_host.trim())
  activePreset.value = hit?.name ?? ''
}

function applyPreset(p: (typeof PRESETS)[number]) {
  form.smtp_host = p.host
  form.smtp_port = p.port
  form.smtp_encryption = p.encryption
  activePreset.value = p.name
  if (p.name === 'SendGrid' && !form.smtp_username) form.smtp_username = 'apikey'
  toast.info(`已填入 ${p.name} 的 SMTP 参数`)
}

/** 切换加密方式时同步默认端口，用户手填过非标准端口就不动。 */
function onEncryptionChange() {
  const standard = ['465', '587', '25']
  if (!standard.includes(form.smtp_port)) return
  form.smtp_port =
    form.smtp_encryption === 'ssl' ? '465' : form.smtp_encryption === 'starttls' ? '587' : '25'
}

function validate(): boolean {
  for (const k of MAIL_KEYS) delete errors[k]

  // 没开启邮件功能时不强制校验 —— 允许先存半份配置
  if (form.smtp_enabled === '1') {
    if (!form.smtp_host.trim()) errors.smtp_host = '开启邮件通知必须填写 SMTP 服务器'
    if (!form.smtp_from_email.trim()) errors.smtp_from_email = '开启邮件通知必须填写发件人邮箱'
  }

  const port = Number(form.smtp_port)
  if (!Number.isInteger(port) || port < 1 || port > 65535) errors.smtp_port = '端口必须是 1-65535'

  if (
    form.smtp_from_email.trim() &&
    !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(form.smtp_from_email.trim())
  ) {
    errors.smtp_from_email = '邮箱格式不正确'
  }

  return Object.keys(errors).length === 0
}

async function save() {
  if (!validate()) {
    tab.value = 'smtp'
    const bad = Object.keys(errors)[0] as MailKey
    toast.error(errors[bad] ?? '请检查填写内容')
    return
  }

  saving.value = true
  try {
    const payload: Record<string, string> = {}
    for (const k of MAIL_KEYS) payload[k] = form[k]
    // 密码原样提交：没改过就是掩码，后端会保留旧值
    await adminApi.updateSettings(payload)
    passwordMask.value = form.smtp_password
    toast.success('邮件配置已保存')
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    saving.value = false
  }
}

function openTest() {
  if (!form.smtp_host.trim()) {
    toast.error('请先填写 SMTP 服务器')
    tab.value = 'smtp'
    return
  }
  testTo.value = testTo.value || form.smtp_from_email
  testVisible.value = true
}

/**
 * 用表单里的当前值发测试信，而不是数据库里已保存的值 ——
 * 这样管理员可以「先测通再保存」，不用拿线上配置反复试错。
 */
async function sendTest() {
  const to = testTo.value.trim()
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(to)) {
    toast.error('收件邮箱格式不正确')
    return
  }

  testing.value = true
  try {
    await adminApi.testMail({
      to,
      host: form.smtp_host.trim(),
      port: Number(form.smtp_port) || 465,
      username: form.smtp_username,
      // 掩码原样传给后端，它会回退到已保存的密码
      password: form.smtp_password,
      from_email: form.smtp_from_email.trim(),
      from_name: form.smtp_from_name,
      encryption: form.smtp_encryption,
    })
    toast.success('测试邮件已发送，请查收（也请检查垃圾邮件箱）')
    testVisible.value = false
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '发送失败')
  } finally {
    testing.value = false
  }
}

/** 把变量插到正文末尾。比让用户手抄 {{}} 靠谱。 */
function appendVar(key: MailKey, token: string) {
  form[key] = form[key] ? `${form[key]}\n${token}` : token
}

onMounted(load)
</script>

<template>
  <div class="space-y-5">
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
      <WdTabs v-model="tab" :tabs="tabs" />
      <div class="flex items-center gap-2">
        <WdButton :disabled="loading" @click="openTest">
          <Send class="w-4 h-4" />
          发送测试邮件
        </WdButton>
        <WdButton variant="primary" :loading="saving" :disabled="loading" @click="save">
          <Save class="w-4 h-4" />
          保存配置
        </WdButton>
      </div>
    </div>

    <!-- mode="out-in"：两个分页高度不同，交叉淡入会让页面跳一下 -->
    <Transition name="wd-tab" mode="out-in">
      <!-- SMTP -->
      <div v-if="tab === 'smtp'" key="smtp" class="space-y-5">
        <WdCard title="邮件通知" subtitle="用于给买家发送支付成功与发货通知">
          <div class="flex items-start justify-between gap-6">
            <div class="min-w-0">
              <div class="flex items-center gap-2">
                <p class="text-sm font-medium text-gray-800">启用邮件通知</p>
                <WdBadge v-if="form.smtp_enabled === '1'" :tone="configured ? 'teal' : 'amber'">
                  {{ configured ? '已配置' : '配置不完整' }}
                </WdBadge>
              </div>
              <p class="mt-1 text-xs text-gray-400 leading-relaxed">
                关闭后系统不会发出任何邮件。自动发货商品仍能正常交付 ——
                用户可以在订单详情页看到卡密。
              </p>
            </div>
            <WdSwitch
              :model-value="form.smtp_enabled === '1'"
              label="启用邮件通知"
              @update:model-value="(v) => (form.smtp_enabled = v ? '1' : '0')"
            />
          </div>
        </WdCard>

        <WdCard title="快速填充" subtitle="选择服务商自动带出服务器地址、端口与加密方式">
          <div class="flex flex-wrap gap-2">
            <button
              v-for="p in PRESETS"
              :key="p.name"
              class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-all duration-200"
              :class="
                activePreset === p.name
                  ? 'bg-[#4a9d9a] text-white shadow-md shadow-[#4a9d9a]/20'
                  : 'bg-[#faf8f5] text-gray-600 hover:bg-[#f3efe9]'
              "
              @click="applyPreset(p)"
            >
              <Zap v-if="activePreset === p.name" class="w-3 h-3" />
              {{ p.name }}
            </button>
          </div>

          <p
            v-for="p in PRESETS.filter((x) => x.name === activePreset && x.note)"
            :key="p.name"
            class="mt-4 px-3.5 py-2.5 rounded-xl bg-[#e8b86d]/15 text-xs text-[#b8873f] leading-relaxed"
          >
            {{ p.name }}：{{ p.note }}
          </p>
        </WdCard>

        <WdCard title="服务器参数">
          <div class="grid md:grid-cols-2 gap-5">
            <WdInput
              v-model="form.smtp_host"
              label="SMTP 服务器"
              placeholder="smtp.example.com"
              :error="errors.smtp_host"
              @change="matchPreset"
            />
            <WdInput
              v-model="form.smtp_encryption"
              type="select"
              label="加密方式"
              :options="ENCRYPTIONS"
              @change="onEncryptionChange"
            />
            <WdInput
              v-model="form.smtp_port"
              type="number"
              label="端口"
              :min="1"
              :max="65535"
              :error="errors.smtp_port"
            />
            <WdInput
              v-model="form.smtp_username"
              label="用户名"
              placeholder="通常是完整邮箱地址"
              hint="留空表示服务器不需要认证"
            />
            <WdInput
              v-model="form.smtp_password"
              type="password"
              label="密码 / 授权码"
              :hint="
                form.smtp_password === passwordMask && passwordMask
                  ? '当前显示的是脱敏值，不修改则保留原密码'
                  : 'QQ / 163 等邮箱需要填授权码，不是登录密码'
              "
            />
            <WdInput
              v-model="form.smtp_from_email"
              label="发件人邮箱"
              placeholder="noreply@example.com"
              :error="errors.smtp_from_email"
              hint="多数服务商要求与用户名一致，否则会被拒收"
            />
            <WdInput
              v-model="form.smtp_from_name"
              label="发件人名称"
              :maxlength="50"
              placeholder="MoeCard"
              hint="收件箱里显示的寄件人"
            />
          </div>

          <p
            class="mt-5 px-3.5 py-2.5 rounded-xl bg-[#faf8f5] text-xs text-gray-500 leading-relaxed"
          >
            邮件发送失败不会影响订单与发货 —— 系统会把错误写进「系统日志 → 邮件日志」，
            订单该发货照样发货。这是刻意的：邮件服务抖动不该拖垮支付流程。
          </p>
        </WdCard>
      </div>

      <!-- 模板 -->
      <div v-else-if="tab === 'templates'" key="templates" class="space-y-5">
        <WdCard v-for="g in templateGroups" :key="g.subjectKey" :title="g.title" :subtitle="g.desc">
          <template v-if="g.toggleKey" #actions>
            <div class="flex items-center gap-2.5">
              <span class="text-xs text-gray-400">{{ g.toggleLabel }}</span>
              <WdSwitch
                :model-value="form[g.toggleKey] === '1'"
                :label="g.toggleLabel"
                @update:model-value="(v) => (form[g.toggleKey as MailKey] = v ? '1' : '0')"
              />
            </div>
          </template>

          <div class="space-y-4">
            <WdInput v-model="form[g.subjectKey]" label="邮件标题" :maxlength="120" />

            <WdInput
              v-model="form[g.bodyKey]"
              type="textarea"
              label="邮件正文（支持 HTML）"
              :rows="10"
              mono
            />

            <div>
              <p class="mb-2 text-xs text-gray-400">可用变量（点击插入到正文末尾）</p>
              <div class="flex flex-wrap gap-2">
                <button
                  v-for="v in g.vars"
                  :key="v.key"
                  class="group inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-[#faf8f5] hover:bg-[#4a9d9a]/10 transition-colors duration-200"
                  :title="`插入 ${v.key}`"
                  @click="appendVar(g.bodyKey, v.key)"
                >
                  <code class="font-mono text-[11px] text-[#4a9d9a]">{{ v.key }}</code>
                  <span class="text-[11px] text-gray-400 group-hover:text-gray-500">{{
                    v.desc
                  }}</span>
                </button>
              </div>
            </div>
          </div>
        </WdCard>

        <p
          class="px-5 py-4 rounded-2xl bg-white shadow-xl shadow-black/[0.04] text-xs text-gray-500 leading-relaxed"
        >
          变量值在渲染时会做 HTML 转义，因此商品名里的
          <code class="font-mono text-[11px] text-gray-600">&lt;</code>
          不会破坏邮件结构，也无法被用来注入脚本。唯一例外是
          <!-- 用变量渲染：模板里直接写 {{…}} 字面量会被 Vue 当成插值解析 -->
          <code class="font-mono text-[11px] text-[#4a9d9a]">{{ DELIVERY_VARS[0].key }}</code>
          中的换行会保留 —— 卡密通常是多行的。
        </p>
      </div>
    </Transition>

    <!-- 测试 -->
    <WdModal v-model="testVisible" title="发送测试邮件" width="460px">
      <div class="space-y-4">
        <WdInput
          v-model="testTo"
          type="email"
          label="收件邮箱"
          required
          placeholder="your@example.com"
          @enter="sendTest"
        />
        <p class="px-3.5 py-2.5 rounded-xl bg-[#faf8f5] text-xs text-gray-500 leading-relaxed">
          使用当前表单里的参数发送，<span class="font-medium text-gray-700">不需要先保存</span>。
          密码字段若仍是脱敏值，会自动使用已保存的密码。
        </p>
      </div>

      <template #footer>
        <WdButton @click="testVisible = false">取消</WdButton>
        <WdButton variant="primary" :loading="testing" @click="sendTest">发送</WdButton>
      </template>
    </WdModal>
  </div>
</template>
