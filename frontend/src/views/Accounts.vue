<template>
 <n-space vertical>
 <n-card>
  <n-space justify="space-between" align="center">
  <h3>云账户</h3>
  <n-button type="primary" :disabled="savePending || testPending" @click="openModal(null)">添加</n-button>
  </n-space>
 </n-card>

 <!-- 桌面表格 -->
 <div class="desktop-view">
  <n-data-table :columns="columns" :data="data" :loading="loading" :scroll-x="720" />
 </div>

 <!-- 移动端列表 -->
 <div class="mobile-view">
  <n-spin :show="loading">
   <n-list hoverable clickable>
    <n-list-item v-for="row in data" :key="row.ID">
     <n-thing :title="row.Name">
      <template #description>
        <n-tag :type="row.Type === '123pan' ? 'info' : 'success'" size="small" style="margin-right: 5px">
          {{ typeLabels[row.Type] || row.Type }}
        </n-tag>
        <n-tag :type="playbackMode(row).type" size="small" style="margin-right: 5px">
         {{ playbackMode(row).label }}
        </n-tag>
        <span style="font-size: 12px; color: #888">缓存: {{ row.CacheTTL }}分</span>
      </template>
      <template #footer>
       <n-space size="small">
         <n-button size="tiny" ghost :disabled="isDeleting(row.ID)" @click.stop="openModal(row)">编辑</n-button>
         <n-button size="tiny" type="error" ghost :loading="isDeleting(row.ID)" :disabled="isDeleting(row.ID)" @click.stop="handleDelete(row)">删除</n-button>
       </n-space>
      </template>
     </n-thing>
    </n-list-item>
    <n-empty v-if="data.length === 0" description="暂无账户" style="margin-top: 20px" />
   </n-list>
  </n-spin>
 </div>

 <n-modal v-model:show="showModal" preset="card" title="账户配置" style="width: 600px; max-width: 95%" :closable="!savePending && !testPending" :mask-closable="!savePending && !testPending" :close-on-esc="!savePending && !testPending">
  <n-form ref="formRef" :model="form" label-placement="top" label-width="auto" :disabled="savePending || testPending || secretPending">
  <n-form-item label="名称" path="Name">
   <n-input v-model:value="form.Name" placeholder="账户备注" />
  </n-form-item>

  <n-form-item label="账户类型" path="Type">
     <n-select v-model:value="form.Type" :options="typeOptions" />
  </n-form-item>

  <n-alert v-if="form.ID" type="info" style="margin-bottom: 16px;">
   {{ secretPending ? '正在读取已保存凭据...' : '点击密码框右侧的眼睛时才会读取并显示已保存凭据；关闭弹窗后会立即从页面中清除。' }}
  </n-alert>

  <template v-if="form.Type === '123pan'">
   <n-form-item label="Client ID">
   <n-input v-model:value="form.ClientID" />
   </n-form-item>
   <n-form-item label="Client Secret">
     <n-input :type="secretVisible.client_secret ? 'text' : 'password'" autocomplete="off" v-model:value="form.ClientSecret" :placeholder="form.ID && form.HasClientSecret ? '点击眼睛查看，或直接输入新值' : 'Client Secret'" @update:value="value => markSecretEdited('client_secret', value)">
      <template #suffix><n-button text :loading="pendingSecretField === 'client_secret'" :disabled="secretPending && pendingSecretField !== 'client_secret'" @mousedown.prevent @click.stop="toggleSecretVisibility('client_secret')"><template #icon><n-icon><EyeInvisibleOutlined v-if="secretVisible.client_secret" /><EyeOutlined v-else /></n-icon></template></n-button></template>
     </n-input>
   </n-form-item>
   <n-alert type="success">默认使用 302 重定向：CloudStream 获取 123 云盘下载地址后将播放器重定向到源站，媒体流量不经过 CloudStream。</n-alert>
  </template>

  <template v-else-if="form.Type === 'openlist'">
   <n-form-item label="URL 地址">
   <n-input v-model:value="form.OpenListURL" placeholder="http://192.168.1.5:5244" />
   </n-form-item>
   
   <n-divider dashed>认证方式 (二选一)</n-divider>
   
    <n-tabs v-model:value="form.OpenListAuthMode" type="segment">
    <n-tab-pane name="password" tab="账号密码">
      <n-form-item label="用户名">
        <n-input v-model:value="form.OpenListUsername" placeholder="admin" />
      </n-form-item>
      <n-form-item label="密码">
         <n-input :type="secretVisible.openlist_password ? 'text' : 'password'" autocomplete="off" v-model:value="form.OpenListPassword" :placeholder="form.ID && form.HasOpenListPassword ? '点击眼睛查看，或直接输入新值' : '密码'" @update:value="value => markSecretEdited('openlist_password', value)">
          <template #suffix><n-button text :loading="pendingSecretField === 'openlist_password'" :disabled="secretPending && pendingSecretField !== 'openlist_password'" @mousedown.prevent @click.stop="toggleSecretVisibility('openlist_password')"><template #icon><n-icon><EyeInvisibleOutlined v-if="secretVisible.openlist_password" /><EyeOutlined v-else /></n-icon></template></n-button></template>
         </n-input>
      </n-form-item>
    </n-tab-pane>
    <n-tab-pane name="token" tab="Token">
      <n-form-item label="Token (长期令牌)">
         <n-input :type="secretVisible.openlist_token ? 'text' : 'password'" autocomplete="off" v-model:value="form.OpenListToken" :placeholder="form.ID && form.HasOpenListToken ? '点击眼睛查看，或直接输入新值' : '长期 Token'" @update:value="value => markSecretEdited('openlist_token', value)">
          <template #suffix><n-button text :loading="pendingSecretField === 'openlist_token'" :disabled="secretPending && pendingSecretField !== 'openlist_token'" @mousedown.prevent @click.stop="toggleSecretVisibility('openlist_token')"><template #icon><n-icon><EyeInvisibleOutlined v-if="secretVisible.openlist_token" /><EyeOutlined v-else /></n-icon></template></n-button></template>
         </n-input>
      </n-form-item>
    </n-tab-pane>
    </n-tabs>
    <n-alert type="success" style="margin-top: 12px;">默认使用 302 重定向：CloudStream 通过 OpenList API 获取 raw_url 后重定向播放器，媒体流量不经过 CloudStream。</n-alert>
  </template>

  <template v-else-if="form.Type === 'webdav'">
   <n-form-item label="WebDAV 地址">
   <n-input v-model:value="form.WebDAVURL" placeholder="http://192.168.1.5:5244/dav" />
   </n-form-item>
   <n-form-item label="用户名">
   <n-input v-model:value="form.WebDAVUsername" placeholder="admin" />
   </n-form-item>
   <n-form-item label="密码">
     <n-input :type="secretVisible.webdav_password ? 'text' : 'password'" autocomplete="off" v-model:value="form.WebDAVPassword" :disabled="form.ClearWebDAVPassword" :placeholder="form.ID && form.HasWebDAVPassword ? '点击眼睛查看，或直接输入新值' : '密码'" @update:value="value => markSecretEdited('webdav_password', value)">
      <template #suffix><n-button text :loading="pendingSecretField === 'webdav_password'" :disabled="form.ClearWebDAVPassword || (secretPending && pendingSecretField !== 'webdav_password')" @mousedown.prevent @click.stop="toggleSecretVisibility('webdav_password')"><template #icon><n-icon><EyeInvisibleOutlined v-if="secretVisible.webdav_password" /><EyeOutlined v-else /></n-icon></template></n-button></template>
     </n-input>
   </n-form-item>
   <n-checkbox v-if="form.ID && form.HasWebDAVPassword" v-model:checked="form.ClearWebDAVPassword">清除已保存的 WebDAV 密码</n-checkbox>
   <n-form-item label="播放方式" style="margin-top: 12px;">
    <n-radio-group v-model:value="form.WebDAVPlaybackMode">
     <n-space>
      <n-radio-button value="proxy">本机代理</n-radio-button>
      <n-radio-button value="upstream-redirect">302 重定向</n-radio-button>
     </n-space>
    </n-radio-group>
    <template #feedback>
     <span v-if="form.WebDAVPlaybackMode === 'upstream-redirect'">适用于文件请求会返回 301/302/303/307/308 的 WebDAV，例如 OpenList 存储策略中的“302 重定向”或“使用代理网址”。CloudStream 只透传 Location，不转发文件正文。若上游直接返回 200/206，请改用本机代理。</span>
     <span v-else>兼容所有 WebDAV，包括上游设置为“本机代理”或直接返回 200/206 的情况。CloudStream 会跟随上游重定向并中转文件正文，因此播放和下载流量会经过本机。</span>
    </template>
   </n-form-item>
  </template>
  
  <n-divider />
  
  <n-form-item label="目录缓存时间 (分钟)">
    <n-input-number v-model:value="form.CacheTTL" :min="0" placeholder="0 表示不缓存" />
    <template #feedback>0 表示不缓存；此存储的缓存过期时间</template>
  </n-form-item>

  <n-form-item label="自定义目录缓存策略">
    <n-input v-model:value="form.CustomCachePolicies" type="textarea" :rows="3" placeholder="" />
    <template #feedback>此存储的缓存过期时间，每行一条，格式：路径模式:分钟，如:/tv/*:10</template>
  </n-form-item>

  <n-form-item label="STRM Base URL">
   <n-input v-model:value="form.StrmBaseURL" placeholder="http://<IP>:12398" />
  </n-form-item>

  <n-space justify="end">
    <n-button :loading="testPending" :disabled="testPending || savePending || secretPending" @click="testConnection">测试连接</n-button>
    <n-button type="primary" :loading="savePending" :disabled="savePending || testPending || secretPending" @click="submit">保存</n-button>
  </n-space>
  </n-form>
 </n-modal>
 </n-space>
</template>

<script setup>
import { ref, reactive, onMounted, h, watch } from 'vue'
import { NButton, NIcon, NSpace, NTag, useMessage, useDialog, NInputNumber } from 'naive-ui'
import api from '../api'
import { EyeInvisibleOutlined, EyeOutlined } from '@vicons/antd'
import { useSecretFields } from '../composables/useSecretFields'

const message = useMessage()
const dialog = useDialog()
const data = ref([])
const loading = ref(false)
const showModal = ref(false)
const savePending = ref(false)
const testPending = ref(false)
const deletingIds = reactive(new Set())
const deleteConfirming = reactive(new Set())
let originalBinding = null
let fetchSeq = 0
const form = reactive({ 
    ID: 0, Name: '', Type: '123pan', 
    ClientID: '', ClientSecret: '', 
    OpenListURL: '', OpenListAuthMode: 'password', OpenListToken: '',
    OpenListUsername: '', OpenListPassword: '',
    WebDAVURL: '', WebDAVUsername: '', WebDAVPassword: '', WebDAVPlaybackMode: 'proxy', ClearWebDAVPassword: false,
    StrmBaseURL: '',
    CacheTTL: 30,
    CustomCachePolicies: ''
})

const typeOptions = [
 { label: '123 云盘开放平台', value: '123pan' },
 { label: 'OpenList (Alist)', value: 'openlist' },
 { label: 'WebDAV', value: 'webdav' }
]
const typeLabels = { '123pan': '123云盘', openlist: 'OpenList', webdav: 'WebDAV' }
const normalizeWebDAVPlaybackMode = (value, legacyDirectLink = false) => {
 if (value === 'proxy' || value === 'upstream-redirect') return value
 return legacyDirectLink === true ? 'upstream-redirect' : 'proxy'
}
const webDAVPlaybackMode = (row) => row?.Type === 'webdav'
 ? normalizeWebDAVPlaybackMode(row.WebDAVPlaybackMode, row.WebDAVDirectLink)
 : 'proxy'
const playbackMode = (row) => {
 if (row.Type === 'webdav' && webDAVPlaybackMode(row) === 'proxy') return { label: '本机代理', type: 'warning' }
 return { label: '302 重定向', type: 'success' }
}

const columns = [
 { title: 'ID', key: 'ID', width: 50 },
 { title: '名称', key: 'Name' },
  { title: '类型', key: 'Type', width: 100, render(row) { return h(NTag, { type: row.Type === '123pan' ? 'info' : 'success', size: 'small' }, { default: () => typeLabels[row.Type] || row.Type }) } },
 { title: '播放', key: 'playback', width: 100, render(row) { const mode = playbackMode(row); return h(NTag, { type: mode.type, size: 'small' }, { default: () => mode.label }) } },
 { title: '缓存', key: 'CacheTTL', width: 80, render(row) { return row.CacheTTL > 0 ? row.CacheTTL + '分' : '无' } },
 { title: '操作', key: 'actions', width: 140, render(row) {
  return h(NSpace, { size: 'small' }, { default: () => [
   h(NButton, { size: 'tiny', disabled: isDeleting(row.ID), onClick: () => openModal(row) }, { default: () => '编辑' }),
   h(NButton, { size: 'tiny', type: 'error', loading: isDeleting(row.ID), disabled: isDeleting(row.ID), onClick: () => handleDelete(row) }, { default: () => '删除' })
  ]})
 }
 }
]

const fetchData = async () => {
 const requestId = ++fetchSeq
 loading.value = true
 try {
  const res = await api.get('/accounts')
  if (requestId === fetchSeq) data.value = res.data || []
 } catch (e) {
 } finally {
  if (requestId === fetchSeq) loading.value = false
 }
}

const normalizeUrl = (value) => {
 const trimmed = (value || '').trim()
 if (!trimmed) return ''
 const lower = trimmed.toLowerCase()
 if (trimmed.includes('://') && !lower.startsWith('http://') && !lower.startsWith('https://')) return ''
 const withScheme = lower.startsWith('http://') || lower.startsWith('https://') ? trimmed : `http://${trimmed}`
 return withScheme.replace(/\/+$/, '')
}

const captureBinding = (row) => ({
 ID: row.ID,
 UpdatedAt: row.UpdatedAt,
 Type: row.Type,
 ClientID: row.ClientID || '',
 HasClientSecret: row.HasClientSecret === true,
 OpenListURL: normalizeUrl(row.OpenListURL),
 OpenListAuthMode: row.OpenListAuthMode || 'password',
 OpenListUsername: (row.OpenListUsername || '').trim(),
 HasOpenListToken: row.HasOpenListToken === true,
 HasOpenListPassword: row.HasOpenListPassword === true,
 WebDAVURL: normalizeUrl(row.WebDAVURL),
 WebDAVUsername: (row.WebDAVUsername || '').trim(),
 HasWebDAVPassword: row.HasWebDAVPassword === true
})

const secretValue = (field) => {
 if (field === 'client_secret') return form.ClientSecret
 if (field === 'openlist_token') return form.OpenListToken
 if (field === 'openlist_password') return form.OpenListPassword
 if (field === 'webdav_password') return form.WebDAVPassword
 return ''
}

const setSecretValue = (field, value) => {
 if (field === 'client_secret') form.ClientSecret = value
 else if (field === 'openlist_token') form.OpenListToken = value
 else if (field === 'openlist_password') form.OpenListPassword = value
 else if (field === 'webdav_password') form.WebDAVPassword = value
}

const hasStoredSecret = (field) => {
 if (field === 'client_secret') return form.Type === '123pan' && form.HasClientSecret
 if (field === 'openlist_token') return form.Type === 'openlist' && form.OpenListAuthMode === 'token' && form.HasOpenListToken
 if (field === 'openlist_password') return form.Type === 'openlist' && form.OpenListAuthMode === 'password' && form.HasOpenListPassword
 if (field === 'webdav_password') return form.Type === 'webdav' && form.HasWebDAVPassword
 return false
}

const storedSecretBindingMatches = (field) => {
 if (!form.ID || !originalBinding || form.ID !== originalBinding.ID || form.UpdatedAt !== originalBinding.UpdatedAt || form.Type !== originalBinding.Type) return false
 if (field === 'client_secret') return form.Type === '123pan' && form.ClientID === originalBinding.ClientID
 if (field === 'openlist_token') return form.Type === 'openlist' && form.OpenListAuthMode === 'token' && originalBinding.OpenListAuthMode === 'token' && normalizeUrl(form.OpenListURL) === originalBinding.OpenListURL
 if (field === 'openlist_password') return form.Type === 'openlist' && form.OpenListAuthMode === 'password' && originalBinding.OpenListAuthMode === 'password' && normalizeUrl(form.OpenListURL) === originalBinding.OpenListURL && form.OpenListUsername.trim() === originalBinding.OpenListUsername
 if (field === 'webdav_password') return form.Type === 'webdav' && normalizeUrl(form.WebDAVURL) === originalBinding.WebDAVURL && form.WebDAVUsername.trim() === originalBinding.WebDAVUsername
 return false
}

const accountSecretContextKey = (field) => {
 const base = { ID: form.ID, UpdatedAt: form.UpdatedAt, accountType: form.Type }
 if (field === 'client_secret') return JSON.stringify({ ...base, clientId: form.ClientID })
 if (field === 'openlist_token') return JSON.stringify({ ...base, openListUrl: normalizeUrl(form.OpenListURL), openListAuthMode: form.OpenListAuthMode })
 if (field === 'openlist_password') return JSON.stringify({ ...base, openListUrl: normalizeUrl(form.OpenListURL), openListAuthMode: form.OpenListAuthMode, openListUsername: form.OpenListUsername.trim() })
 return JSON.stringify({ ...base, webdavUrl: normalizeUrl(form.WebDAVURL), webdavUsername: form.WebDAVUsername.trim() })
}

const revealAccountSecret = async (field) => {
 if (!storedSecretBindingMatches(field)) {
  message.warning('账户地址或身份已变更，请直接输入新凭据')
  throw new Error('secret binding changed')
 }
 const res = await api.post(`/accounts/${form.ID}/secrets/reveal`, {
  field,
  updatedAt: form.UpdatedAt,
  accountType: form.Type,
  clientId: form.ClientID,
  openListUrl: normalizeUrl(form.OpenListURL),
  openListAuthMode: form.OpenListAuthMode,
  openListUsername: form.OpenListUsername.trim(),
  webdavUrl: normalizeUrl(form.WebDAVURL),
  webdavUsername: form.WebDAVUsername.trim()
 })
 return res.data?.value ?? ''
}

const {
 visible: secretVisible,
 pending: secretPending,
 pendingField: pendingSecretField,
 toggle: toggleSecretVisibility,
 markEdited: markSecretEdited,
 clearField: clearSecretField,
 reset: resetSecrets,
 valueForSubmit: secretValueForSubmit,
 hasExplicitValue: hasExplicitSecret
} = useSecretFields({
 fields: ['client_secret', 'openlist_token', 'openlist_password', 'webdav_password'],
 getValue: secretValue,
 setValue: setSecretValue,
 hasStoredValue: hasStoredSecret,
 reveal: revealAccountSecret,
 contextKey: accountSecretContextKey
})

const preparePayload = () => {
 const webDAVMode = form.Type === 'webdav'
  ? normalizeWebDAVPlaybackMode(form.WebDAVPlaybackMode)
  : 'proxy'
 const payload = {
  ...form,
  ClientSecret: secretValueForSubmit('client_secret'),
  OpenListToken: secretValueForSubmit('openlist_token'),
  OpenListPassword: secretValueForSubmit('openlist_password'),
  WebDAVPassword: secretValueForSubmit('webdav_password'),
  WebDAVURL: form.Type === 'webdav' ? normalizeUrl(form.WebDAVURL) : form.WebDAVURL,
  WebDAVPlaybackMode: webDAVMode,
  WebDAVDirectLink: webDAVMode === 'upstream-redirect'
 }
 if (form.Type !== '123pan') payload.ClientSecret = ''
 if (form.Type !== 'openlist' || form.OpenListAuthMode !== 'token') payload.OpenListToken = ''
 if (form.Type !== 'openlist' || form.OpenListAuthMode !== 'password') payload.OpenListPassword = ''
 if (form.Type !== 'webdav') payload.WebDAVPassword = ''
 return payload
}

const prepareTestPayload = () => {
 const payload = preparePayload()
  if (form.Type === 'webdav' && (form.ClearWebDAVPassword || (!hasExplicitSecret('webdav_password') && originalBinding && !originalBinding.HasWebDAVPassword))) {
   payload.ID = 0
   payload.WebDAVPassword = ''
  }
 return payload
}

const clearSecrets = () => {
 resetSecrets()
 form.ClearWebDAVPassword = false
 originalBinding = null
}

watch(showModal, (visible) => {
 if (!visible) clearSecrets()
}, { flush: 'sync' })

watch(() => [form.Type, form.OpenListAuthMode], ([type, authMode]) => {
 if (type !== '123pan') clearSecretField('client_secret')
 if (type !== 'openlist' || authMode !== 'token') clearSecretField('openlist_token')
 if (type !== 'openlist' || authMode !== 'password') clearSecretField('openlist_password')
 if (type !== 'webdav') clearSecretField('webdav_password')
}, { flush: 'sync' })

watch(() => form.ClearWebDAVPassword, (clear) => {
 if (!clear) return
 clearSecretField('webdav_password')
})

const showResponseWarning = (res) => {
 if (typeof res?.warning === 'string' && res.warning.trim()) {
  message.warning(res.warning)
 }
}

const openModal = (row) => {
 resetSecrets()
 if (row) {
  const { WebDAVDirectLink: legacyDirectLink, ...account } = row
  Object.assign(form, account, {
   ClientSecret: '', OpenListToken: '', OpenListPassword: '', WebDAVPassword: '',
   WebDAVPlaybackMode: row.Type === 'webdav' ? normalizeWebDAVPlaybackMode(row.WebDAVPlaybackMode, legacyDirectLink) : 'proxy',
   ClearWebDAVPassword: false
  })
  originalBinding = captureBinding(row)
 } else {
  Object.assign(form, {
    ID: 0, Name: '', Type: '123pan', 
    ClientID: '', ClientSecret: '', 
     OpenListURL: '', OpenListAuthMode: 'password', OpenListToken: '',
    OpenListUsername: '', OpenListPassword: '',
    WebDAVURL: '', WebDAVUsername: '', WebDAVPassword: '', WebDAVPlaybackMode: 'proxy', ClearWebDAVPassword: false,
    StrmBaseURL: '',
    CacheTTL: 30,
    CustomCachePolicies: ''
  })
  originalBinding = null
 }
 showModal.value = true
}

const validateSecretBinding = () => {
 const typeChanged = Boolean(form.ID && originalBinding && form.Type !== originalBinding.Type)

 if (form.Type === 'webdav' && !normalizeUrl(form.WebDAVURL)) {
  message.warning('请输入有效的 HTTP(S) WebDAV 地址')
  return false
 }

 if (typeChanged) {
  if (form.Type === '123pan' && (!form.ClientID.trim() || !hasExplicitSecret('client_secret'))) {
   message.warning('切换为 123 云盘时必须填写 Client ID 和 Client Secret')
   return false
  }
  if (form.Type === 'openlist') {
   if (!normalizeUrl(form.OpenListURL)) {
    message.warning('切换为 OpenList 时必须填写地址')
    return false
   }
   if (form.OpenListAuthMode === 'token' && !hasExplicitSecret('openlist_token')) {
    message.warning('切换为 OpenList Token 认证时必须填写 Token')
    return false
   }
   if (form.OpenListAuthMode === 'password' && (!form.OpenListUsername.trim() || !hasExplicitSecret('openlist_password'))) {
    message.warning('切换为 OpenList 账号密码认证时必须填写用户名和密码')
    return false
   }
  }
  if (form.Type === 'webdav' && !normalizeUrl(form.WebDAVURL)) {
   message.warning('切换为 WebDAV 时必须填写地址')
   return false
  }
  return true
 }

 if (!form.ID || !originalBinding) return true

 if (form.Type === '123pan' && originalBinding.HasClientSecret && !hasExplicitSecret('client_secret') && form.ClientID !== originalBinding.ClientID) {
  message.warning('Client ID 已变更，请重新输入 Client Secret')
  return false
 }
 if (form.Type === 'openlist') {
  const modeChanged = form.OpenListAuthMode !== originalBinding.OpenListAuthMode
  const urlChanged = normalizeUrl(form.OpenListURL) !== originalBinding.OpenListURL
  if (form.OpenListAuthMode === 'token' && !hasExplicitSecret('openlist_token') && (modeChanged || (originalBinding.HasOpenListToken && urlChanged))) {
   message.warning('OpenList 地址或认证方式已变更，请重新输入 Token')
   return false
  }
  const usernameChanged = form.OpenListUsername.trim() !== originalBinding.OpenListUsername
  if (form.OpenListAuthMode === 'password' && !hasExplicitSecret('openlist_password') && (modeChanged || (originalBinding.HasOpenListPassword && (urlChanged || usernameChanged)))) {
   message.warning('OpenList 地址、认证方式或用户名已变更，请重新输入密码')
   return false
  }
 }
  if (form.Type === 'webdav' && originalBinding.HasWebDAVPassword && !form.ClearWebDAVPassword && !hasExplicitSecret('webdav_password')) {
  const addressChanged = normalizeUrl(form.WebDAVURL) !== originalBinding.WebDAVURL
  const usernameChanged = form.WebDAVUsername.trim() !== originalBinding.WebDAVUsername
  if (addressChanged || usernameChanged) {
   message.warning('WebDAV 地址或用户名已变更，请重新输入密码')
   return false
  }
 }
 return true
}

const testConnection = async () => {
 if (testPending.value || savePending.value || secretPending.value || !validateSecretBinding()) return
 testPending.value = true
 try {
  const res = await api.post('/accounts/test', prepareTestPayload())
  message.success(res.message)
  showResponseWarning(res)
 } catch (e) {
 } finally {
  testPending.value = false
 }
}

const submit = async () => {
 if (savePending.value || testPending.value || secretPending.value || !validateSecretBinding()) return
 savePending.value = true
 try {
  const payload = preparePayload()
  const res = form.ID
   ? await api.put(`/accounts/${form.ID}`, payload)
   : await api.post('/accounts', payload)
  message.success('保存成功')
  showResponseWarning(res)
  showModal.value = false
  clearSecrets()
  await fetchData()
 } catch (e) {
 } finally {
  savePending.value = false
 }
}

const isDeleting = (id) => deletingIds.has(id) || deleteConfirming.has(id)

const handleDelete = (row) => {
 if (isDeleting(row.ID)) return
 deleteConfirming.add(row.ID)
 dialog.warning({
 title: '警告', content: '删除账户会将关联任务一起删除。', positiveText: '删除', negativeText: '取消', closable: false, maskClosable: false, closeOnEsc: false,
 onNegativeClick: () => deleteConfirming.delete(row.ID),
 onPositiveClick: async () => {
  deleteConfirming.delete(row.ID)
  if (deletingIds.has(row.ID)) return
  deletingIds.add(row.ID)
  try {
   const res = await api.delete(`/accounts/${row.ID}`)
   message.success('删除成功')
   showResponseWarning(res)
   await fetchData()
  } catch (e) {
  } finally {
   deletingIds.delete(row.ID)
  }
 }
 })
}

onMounted(fetchData)
</script>

<style scoped>
.mobile-view { display: none; }
.desktop-view { display: block; }
@media (max-width: 600px) {
 .desktop-view { display: none; }
 .mobile-view { display: block; }
}
</style>
