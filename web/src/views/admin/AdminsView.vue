<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ImagePlus, KeyRound, Loader2, Plus, ShieldCheck, Smartphone } from 'lucide-vue-next'
import { ApiError, adminApi } from '@/api'
import type { Admin, TOTPStatus } from '@/api/types'
import { useAuthStore } from '@/stores/auth'
import { copyText, formatDateTime } from '@/utils/format'
import {
  WdBadge,
  WdButton,
  WdCard,
  WdInput,
  WdModal,
  WdPagination,
  WdTable,
  confirmDialog,
  toast,
  type Column,
} from '@/ui'

const auth = useAuthStore()

const list = ref<Admin[]>([])
const total = ref(0)
const loading = ref(false)
const query = reactive({ page: 1, page_size: 20 })

const dialogVisible = ref(false)
const editing = ref<Admin | null>(null)
const submitting = ref(false)

const form = reactive({
  username: '',
  password: '',
  nickname: '',
  avatar: '',
  status: 'active' as 'active' | 'disabled',
})
const avatarUploading = ref(false)

async function onPickAvatar(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  avatarUploading.value = true
  try {
    const res = await adminApi.upload(file)
    form.avatar = res.url
  } catch {
    toast.error('头像上传失败')
  } finally {
    avatarUploading.value = false
    // 清空后同一张图能再选一次，否则第二次选它不会触发 change
    input.value = ''
  }
}
const errors = reactive<Record<string, string>>({})

// ---- 两步验证 ----
const totp = ref<TOTPStatus>({ enabled: false, recovery_remaining: 0 })
const totpVisible = ref(false)
const totpStep = ref<'intro' | 'scan' | 'codes'>('intro')
const totpSecret = ref('')
const totpQR = ref('')
const totpCode = ref('')
const totpBusy = ref(false)
const recoveryCodes = ref<string[]>([])
const disablePwd = ref('')

async function loadTOTP() {
  try {
    totp.value = await adminApi.totpStatus()
  } catch {
    /* 状态查询失败不影响页面其余部分 */
  }
}

async function openTOTP() {
  totpCode.value = ''
  disablePwd.value = ''
  recoveryCodes.value = []
  totpStep.value = 'intro'
  totpVisible.value = true
}

async function beginSetup() {
  totpBusy.value = true
  try {
    const res = await adminApi.totpSetup()
    totpSecret.value = res.secret
    // qrcode 体积不小，只在真正要用时才动态加载
    const QR = await import('qrcode')
    totpQR.value = await QR.toDataURL(res.uri, { width: 200, margin: 1 })
    totpStep.value = 'scan'
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '获取二维码失败')
  } finally {
    totpBusy.value = false
  }
}

async function confirmEnable() {
  if (!/^\d{6}$/.test(totpCode.value.trim())) {
    toast.error('请输入 6 位验证码')
    return
  }
  totpBusy.value = true
  try {
    const res = await adminApi.totpEnable(totpCode.value.trim())
    recoveryCodes.value = res.recovery_codes
    totpStep.value = 'codes'
    await loadTOTP()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '开启失败')
  } finally {
    totpBusy.value = false
  }
}

async function doDisable() {
  if (!disablePwd.value) {
    toast.error('请输入当前密码')
    return
  }
  totpBusy.value = true
  try {
    await adminApi.totpDisable(disablePwd.value)
    toast.success('两步验证已关闭')
    totpVisible.value = false
    await loadTOTP()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '关闭失败')
  } finally {
    totpBusy.value = false
  }
}

async function copyRecovery() {
  const ok = await copyText(recoveryCodes.value.join('\n'))
  ok ? toast.success('恢复码已复制') : toast.error('复制失败')
}

/** 开启 2FA 会作废所有会话，必须重新登录 */
function finishSetup() {
  totpVisible.value = false
  toast.info('两步验证已开启，请重新登录')
  setTimeout(() => auth.logout(), 1200)
}

// 修改自己的密码
const pwdVisible = ref(false)
const pwdSubmitting = ref(false)
const pwd = reactive({ old_password: '', new_password: '', confirm: '' })
const pwdErrors = reactive<Record<string, string>>({})

const columns: Column[] = [
  { key: 'username', label: '账号', width: '220px' },
  { key: 'status', label: '状态', width: '90px', align: 'center' },
  { key: 'last_login', label: '最近登录' },
  { key: 'created_at', label: '创建时间', width: '160px', hideOnMobile: true },
  { key: 'actions', label: '操作', width: '150px', align: 'center' },
]

/** 启用中的管理员数量。用于阻止把最后一个可用账号禁用/删除。 */
const activeCount = computed(() => list.value.filter((a) => a.status === 'active').length)

function isSelf(row: Admin) {
  return auth.admin?.id === row.id
}

async function load() {
  loading.value = true
  try {
    const res = await adminApi.admins({ page: query.page, page_size: query.page_size })
    list.value = res.list ?? []
    total.value = res.total
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = null
  Object.assign(form, {
    username: '',
    password: '',
    nickname: '',
    avatar: '',
    status: 'active',
  })
  for (const k of Object.keys(errors)) delete errors[k]
  dialogVisible.value = true
}

function openEdit(row: Admin) {
  editing.value = row
  Object.assign(form, {
    username: row.username,
    password: '',
    nickname: row.nickname,
    avatar: row.avatar || '',
    status: row.status,
  })
  for (const k of Object.keys(errors)) delete errors[k]
  dialogVisible.value = true
}

function validate(): boolean {
  for (const k of Object.keys(errors)) delete errors[k]

  const u = form.username.trim()
  if (!editing.value) {
    if (u.length < 3 || u.length > 32) errors.username = '用户名长度需在 3-32 之间'
    if (!/^[A-Za-z0-9_.-]+$/.test(u)) errors.username = '用户名只能包含字母、数字与 _ . -'
  }

  // 新建必须设密码；编辑时留空表示不改
  if (!editing.value || form.password) {
    const p = form.password
    if (p.length < 8) errors.password = '密码至少 8 位'
    else if (!/[A-Za-z]/.test(p) || !/[0-9]/.test(p)) errors.password = '密码需同时包含字母和数字'
  }

  return Object.keys(errors).length === 0
}

async function submit() {
  if (!validate()) return

  // 把自己禁用等于当场把自己锁在门外
  if (editing.value && isSelf(editing.value) && form.status === 'disabled') {
    toast.error('不能禁用当前登录的账号')
    return
  }
  if (
    editing.value &&
    editing.value.status === 'active' &&
    form.status === 'disabled' &&
    activeCount.value <= 1
  ) {
    toast.error('至少要保留一个启用中的管理员')
    return
  }

  submitting.value = true
  try {
    if (editing.value) {
      const payload: Record<string, unknown> = {
        nickname: form.nickname.trim(),
        avatar: form.avatar.trim(),
        status: form.status,
      }
      // 密码留空 = 不改，避免把别人的密码重置成空串
      if (form.password) payload.password = form.password
      await adminApi.updateAdmin(editing.value.id, payload)
      toast.success('管理员已更新')
    } else {
      await adminApi.createAdmin({
        username: form.username.trim(),
        password: form.password,
        nickname: form.nickname.trim(),
        avatar: form.avatar.trim(),
        status: form.status,
      })
      toast.success('管理员已创建')
    }
    dialogVisible.value = false
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    submitting.value = false
  }
}

async function remove(row: Admin) {
  if (isSelf(row)) {
    toast.error('不能删除当前登录的账号')
    return
  }
  if (row.status === 'active' && activeCount.value <= 1) {
    toast.error('至少要保留一个启用中的管理员')
    return
  }

  const ok = await confirmDialog({
    title: '删除管理员',
    message: `确定删除管理员「${row.username}」吗？该账号将立即无法登录。`,
    confirmText: '删除',
    tone: 'danger',
  })
  if (!ok) return

  try {
    await adminApi.deleteAdmin(row.id)
    toast.success('已删除')
    // 删掉当前页最后一条时回退一页，否则会停在空列表上
    if (list.value.length === 1 && query.page > 1) query.page--
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '删除失败')
  }
}

function openPassword() {
  Object.assign(pwd, { old_password: '', new_password: '', confirm: '' })
  for (const k of Object.keys(pwdErrors)) delete pwdErrors[k]
  pwdVisible.value = true
}

async function changePassword() {
  for (const k of Object.keys(pwdErrors)) delete pwdErrors[k]

  if (!pwd.old_password) pwdErrors.old_password = '请输入当前密码'
  if (pwd.new_password.length < 8) pwdErrors.new_password = '新密码至少 8 位'
  else if (!/[A-Za-z]/.test(pwd.new_password) || !/[0-9]/.test(pwd.new_password)) {
    pwdErrors.new_password = '新密码需同时包含字母和数字'
  } else if (pwd.new_password === pwd.old_password) {
    pwdErrors.new_password = '新密码不能与当前密码相同'
  }
  if (pwd.confirm !== pwd.new_password) pwdErrors.confirm = '两次输入不一致'

  if (Object.keys(pwdErrors).length) return

  pwdSubmitting.value = true
  try {
    await adminApi.changePassword(pwd.old_password, pwd.new_password)
    pwdVisible.value = false
    toast.success('密码已修改，请重新登录')
    // 后端会作废旧 token，本地跟着登出，避免拿着失效 token 到处报错
    setTimeout(() => auth.logout(), 800)
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '修改失败')
  } finally {
    pwdSubmitting.value = false
  }
}

onMounted(() => {
  load()
  loadTOTP()
})
</script>

<template>
  <div class="space-y-5">
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
      <p class="text-xs text-gray-400">
        共 <span class="tabular text-gray-600">{{ total }}</span> 个账号，其中
        <span class="tabular text-gray-600">{{ activeCount }}</span> 个启用中
      </p>
      <div class="flex items-center gap-2">
        <WdButton @click="openTOTP">
          <Smartphone class="w-4 h-4" />
          两步验证
          <WdBadge :tone="totp.enabled ? 'teal' : 'gray'">
            {{ totp.enabled ? '已开启' : '未开启' }}
          </WdBadge>
        </WdButton>
        <WdButton @click="openPassword">
          <KeyRound class="w-4 h-4" />
          修改我的密码
        </WdButton>
        <WdButton variant="primary" @click="openCreate">
          <Plus class="w-4 h-4" />
          添加管理员
        </WdButton>
      </div>
    </div>

    <WdCard flush>
      <div class="px-6 py-5">
        <WdTable :columns="columns" :rows="list" :loading="loading">
          <template #username="{ row }">
            <div class="flex items-center gap-3">
              <img
                v-if="row.avatar"
                :src="row.avatar"
                :alt="row.nickname || row.username"
                class="w-9 h-9 rounded-xl object-cover shrink-0"
              />
              <span
                v-else
                class="w-9 h-9 rounded-xl bg-[#4a9d9a]/10 text-[#4a9d9a] flex items-center justify-center text-sm font-medium shrink-0"
              >
                {{ (row.nickname || row.username).slice(0, 1).toUpperCase() }}
              </span>
              <div class="min-w-0">
                <div class="flex items-center gap-1.5">
                  <p class="text-sm font-medium text-gray-800 truncate">{{ row.username }}</p>
                  <ShieldCheck v-if="isSelf(row as Admin)" class="w-3.5 h-3.5 text-[#4a9d9a] shrink-0" />
                </div>
                <p class="text-xs text-gray-400 truncate">
                  {{ row.nickname || '未设置昵称' }}{{ isSelf(row as Admin) ? '（当前登录）' : '' }}
                </p>
              </div>
            </div>
          </template>

          <template #status="{ row }">
            <WdBadge :tone="row.status === 'active' ? 'teal' : 'gray'" dot>
              {{ row.status === 'active' ? '启用' : '禁用' }}
            </WdBadge>
          </template>

          <template #last_login="{ row }">
            <div v-if="row.last_login_at">
              <p class="text-sm text-gray-600 tabular">{{ formatDateTime(row.last_login_at) }}</p>
              <p v-if="row.last_login_ip" class="text-xs text-gray-400 font-mono">
                {{ row.last_login_ip }}
              </p>
            </div>
            <span v-else class="text-sm text-gray-300">从未登录</span>
          </template>

          <template #created_at="{ row }">
            <span class="text-sm text-gray-500 tabular">{{ formatDateTime(row.created_at) }}</span>
          </template>

          <template #actions="{ row }">
            <div class="flex items-center justify-center gap-3">
              <button
                class="text-xs font-medium text-[#4a9d9a] hover:underline"
                @click="openEdit(row as Admin)"
              >
                编辑
              </button>
              <button
                class="text-xs font-medium text-[#c17767] hover:underline disabled:text-gray-300 disabled:no-underline disabled:cursor-not-allowed"
                :disabled="isSelf(row as Admin)"
                :title="isSelf(row as Admin) ? '不能删除当前登录的账号' : ''"
                @click="remove(row as Admin)"
              >
                删除
              </button>
            </div>
          </template>

          <template #empty>
            <span>还没有其他管理员</span>
          </template>
        </WdTable>

        <WdPagination
          v-model:page="query.page"
          v-model:page-size="query.page_size"
          :total="total"
          @change="load"
        />
      </div>
    </WdCard>

    <!-- 新建 / 编辑 -->
    <WdModal
      v-model="dialogVisible"
      :title="editing ? `编辑 ${editing.username}` : '添加管理员'"
      width="460px"
      :close-on-overlay="false"
    >
      <div class="space-y-4">
        <WdInput
          v-model="form.username"
          label="用户名"
          required
          :disabled="!!editing"
          :maxlength="32"
          :error="errors.username"
          placeholder="3-32 位，字母数字与 _ . -"
          :hint="editing ? '用户名创建后不可修改' : undefined"
        />

        <WdInput v-model="form.nickname" label="昵称" :maxlength="32" placeholder="可选，显示在后台右上角" />

        <div class="flex items-center gap-4">
          <img
            v-if="form.avatar"
            :src="form.avatar"
            alt="头像预览"
            class="w-14 h-14 rounded-2xl object-cover shrink-0 border border-gray-200"
          />
          <span
            v-else
            class="w-14 h-14 rounded-2xl bg-[#4a9d9a]/10 text-[#4a9d9a] grid place-items-center text-lg font-medium shrink-0"
          >
            {{ (form.nickname || form.username || 'A').slice(0, 1).toUpperCase() }}
          </span>
          <div class="flex flex-wrap gap-2">
            <label
              class="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-xl bg-white border border-gray-200 text-sm text-gray-600 cursor-pointer hover:border-[#4a9d9a]/40 hover:text-[#4a9d9a] transition-all duration-200"
            >
              <Loader2 v-if="avatarUploading" class="w-4 h-4 animate-spin" />
              <ImagePlus v-else class="w-4 h-4" />
              {{ form.avatar ? '换一张' : '上传头像' }}
              <input type="file" accept="image/*" class="hidden" @change="onPickAvatar" />
            </label>
            <WdButton v-if="form.avatar" @click="form.avatar = ''">移除</WdButton>
          </div>
        </div>

        <WdInput
          v-model="form.password"
          type="password"
          :label="editing ? '重置密码' : '密码'"
          :required="!editing"
          :error="errors.password"
          :hint="editing ? '留空表示不修改密码' : '至少 8 位，需包含字母和数字'"
        />

        <WdInput
          v-model="form.status"
          type="select"
          label="状态"
          :options="[
            { label: '启用', value: 'active' },
            { label: '禁用', value: 'disabled' },
          ]"
          hint="禁用后该账号无法登录，已登录的会话在下次请求时失效"
        />
      </div>

      <template #footer>
        <WdButton @click="dialogVisible = false">取消</WdButton>
        <WdButton variant="primary" :loading="submitting" @click="submit">保存</WdButton>
      </template>
    </WdModal>

    <!-- 修改自己的密码 -->
    <!-- 两步验证 -->
    <WdModal
      v-model="totpVisible"
      title="两步验证"
      width="480px"
      :close-on-overlay="totpStep !== 'codes'"
    >
      <!-- 已开启：只能关闭 -->
      <template v-if="totp.enabled && totpStep === 'intro'">
        <div class="flex items-start gap-2.5 px-4 py-3.5 rounded-xl bg-[#4a9d9a]/10 text-[#4a9d9a]">
          <ShieldCheck class="w-4 h-4 mt-0.5 shrink-0" />
          <p class="text-sm leading-relaxed">
            两步验证已开启，剩余 {{ totp.recovery_remaining }} 个恢复码可用。
          </p>
        </div>
        <div class="mt-5 space-y-4">
          <p class="text-sm text-gray-500 leading-relaxed">
            关闭后，只要拿到你的密码就能登录后台。这里存着支付密钥和全部卡密，建议保持开启。
          </p>
          <WdInput v-model="disablePwd" type="password" label="当前密码" required />
        </div>
      </template>

      <!-- 未开启：介绍 -->
      <template v-else-if="totpStep === 'intro'">
        <p class="text-sm text-gray-600 leading-relaxed">
          开启后，登录除了密码还需要手机验证码。即使密码泄露，别人也进不来。
        </p>
        <ul class="mt-4 space-y-2 text-xs text-gray-500 leading-relaxed list-disc list-inside">
          <li>需要一个验证器 App：Google Authenticator、1Password、Authy 等都可以</li>
          <li>开启后会生成 8 个一次性恢复码，手机丢了靠它进后台</li>
          <li>开启会让当前所有登录状态失效，需要重新登录一次</li>
        </ul>
      </template>

      <!-- 扫码 -->
      <template v-else-if="totpStep === 'scan'">
        <div class="flex flex-col items-center">
          <div class="p-3 bg-white rounded-2xl border border-gray-100">
            <img v-if="totpQR" :src="totpQR" alt="两步验证二维码" class="w-44 h-44" />
          </div>
          <p class="mt-3 text-sm text-gray-600">用验证器 App 扫描上面的二维码</p>
          <p class="mt-3 text-xs text-gray-500">扫不了？手动输入这串密钥：</p>
          <code class="mt-1 px-3 py-1.5 rounded-lg bg-[#faf8f5] font-mono text-xs text-gray-700 break-all">
            {{ totpSecret }}
          </code>
        </div>
        <div class="mt-5">
          <WdInput
            v-model="totpCode"
            label="输入 App 显示的 6 位验证码"
            required
            :maxlength="6"
            placeholder="000000"
            mono
            @enter="confirmEnable"
          />
        </div>
      </template>

      <!-- 恢复码 -->
      <template v-else>
        <div class="flex items-start gap-2.5 px-4 py-3.5 rounded-xl bg-[#e8b86d]/15 text-[#8f7243]">
          <ShieldCheck class="w-4 h-4 mt-0.5 shrink-0" />
          <p class="text-sm leading-relaxed">
            请立刻保存这 8 个恢复码。它们只显示这一次，关掉后无法再次查看。
            手机丢失时，恢复码是你进入后台的唯一途径。
          </p>
        </div>
        <div class="mt-4 grid grid-cols-2 gap-2">
          <code
            v-for="c in recoveryCodes"
            :key="c"
            class="px-3 py-2 rounded-lg bg-[#faf8f5] font-mono text-sm text-center text-gray-700"
          >
            {{ c }}
          </code>
        </div>
        <WdButton class="mt-4" block @click="copyRecovery">复制全部恢复码</WdButton>
      </template>

      <template #footer>
        <template v-if="totpStep === 'codes'">
          <WdButton variant="primary" @click="finishSetup">我已保存，重新登录</WdButton>
        </template>
        <template v-else-if="totp.enabled">
          <WdButton @click="totpVisible = false">取消</WdButton>
          <WdButton variant="danger" :loading="totpBusy" @click="doDisable">关闭两步验证</WdButton>
        </template>
        <template v-else-if="totpStep === 'scan'">
          <WdButton @click="totpStep = 'intro'">上一步</WdButton>
          <WdButton variant="primary" :loading="totpBusy" @click="confirmEnable">
            确认开启
          </WdButton>
        </template>
        <template v-else>
          <WdButton @click="totpVisible = false">取消</WdButton>
          <WdButton variant="primary" :loading="totpBusy" @click="beginSetup">开始设置</WdButton>
        </template>
      </template>
    </WdModal>

    <WdModal v-model="pwdVisible" title="修改我的密码" width="420px" :close-on-overlay="false">
      <div class="space-y-4">
        <WdInput
          v-model="pwd.old_password"
          type="password"
          label="当前密码"
          required
          :error="pwdErrors.old_password"
        />
        <WdInput
          v-model="pwd.new_password"
          type="password"
          label="新密码"
          required
          :error="pwdErrors.new_password"
          hint="至少 8 位，需包含字母和数字"
        />
        <WdInput
          v-model="pwd.confirm"
          type="password"
          label="确认新密码"
          required
          :error="pwdErrors.confirm"
          @enter="changePassword"
        />
        <p class="px-3.5 py-2.5 rounded-xl bg-[#e8b86d]/15 text-xs text-[#b8873f] leading-relaxed">
          修改成功后当前登录状态会失效，需要用新密码重新登录。
        </p>
      </div>

      <template #footer>
        <WdButton @click="pwdVisible = false">取消</WdButton>
        <WdButton variant="primary" :loading="pwdSubmitting" @click="changePassword">
          确认修改
        </WdButton>
      </template>
    </WdModal>
  </div>
</template>
