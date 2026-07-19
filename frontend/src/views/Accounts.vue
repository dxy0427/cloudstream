<template>
 <n-space vertical>
 <n-card>
  <n-space justify="space-between" align="center">
  <h3>云账户</h3>
  <n-button type="primary" :disabled="detailPending || savePending || testPending" @click="openModal(null)">添加</n-button>
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
          <n-button size="tiny" ghost :loading="isDetailLoading(row.ID)" :disabled="detailPending || isDeleting(row.ID)" @click.stop="openModal(row)">编辑</n-button>
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
   <n-form ref="formRef" :model="form" label-placement="top" label-width="auto" :disabled="savePending || testPending">
  <n-form-item label="名称" path="Name">
   <n-input v-model:value="form.Name" placeholder="账户备注" />
  </n-form-item>

  <n-form-item label="账户类型" path="Type">
     <n-select v-model:value="form.Type" :options="typeOptions" />
  </n-form-item>

  <template v-if="form.Type === '123pan'">
   <n-form-item label="Client ID">
   <n-input v-model:value="form.ClientID" />
   </n-form-item>
   <n-form-item label="Client Secret">
       <n-input :type="secretVisible.client_secret ? 'text' : 'password'" autocomplete="off" v-model:value="form.ClientSecret" placeholder="Client Secret">
       <template #suffix><n-button text @mousedown.prevent @click.stop="toggleSecretVisibility('client_secret')"><template #icon><n-icon><EyeInvisibleOutlined v-if="secretVisible.client_secret" /><EyeOutlined v-else /></n-icon></template></n-button></template>
     </n-input>
   </n-form-item>
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
           <n-input :type="secretVisible.openlist_password ? 'text' : 'password'" autocomplete="off" v-model:value="form.OpenListPassword" placeholder="密码">
           <template #suffix><n-button text @mousedown.prevent @click.stop="toggleSecretVisibility('openlist_password')"><template #icon><n-icon><EyeInvisibleOutlined v-if="secretVisible.openlist_password" /><EyeOutlined v-else /></n-icon></template></n-button></template>
         </n-input>
      </n-form-item>
    </n-tab-pane>
    <n-tab-pane name="token" tab="Token">
      <n-form-item label="Token (长期令牌)">
           <n-input :type="secretVisible.openlist_token ? 'text' : 'password'" autocomplete="off" v-model:value="form.OpenListToken" placeholder="长期 Token">
           <template #suffix><n-button text @mousedown.prevent @click.stop="toggleSecretVisibility('openlist_token')"><template #icon><n-icon><EyeInvisibleOutlined v-if="secretVisible.openlist_token" /><EyeOutlined v-else /></n-icon></template></n-button></template>
         </n-input>
      </n-form-item>
    </n-tab-pane>
    </n-tabs>
  </template>

  <template v-else-if="form.Type === 'webdav'">
   <n-form-item label="WebDAV 地址">
   <n-input v-model:value="form.WebDAVURL" placeholder="http://192.168.1.5:5244/dav" />
   </n-form-item>
   <n-form-item label="用户名">
   <n-input v-model:value="form.WebDAVUsername" placeholder="admin" />
   </n-form-item>
   <n-form-item label="密码">
       <n-input :type="secretVisible.webdav_password ? 'text' : 'password'" autocomplete="off" v-model:value="form.WebDAVPassword" placeholder="密码（可选）">
        <template #suffix><n-button text @mousedown.prevent @click.stop="toggleSecretVisibility('webdav_password')"><template #icon><n-icon><EyeInvisibleOutlined v-if="secretVisible.webdav_password" /><EyeOutlined v-else /></n-icon></template></n-button></template>
      </n-input>
    </n-form-item>
   </template>

   <n-form-item label="播放方式">
    <n-radio-group v-model:value="form.PlaybackMode">
     <n-space>
      <n-radio-button value="proxy">本机代理</n-radio-button>
      <n-radio-button value="redirect">302 重定向</n-radio-button>
     </n-space>
    </n-radio-group>
   </n-form-item>
  
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

  <n-form-item>
   <n-checkbox v-model:checked="form.EnableStreamSign">签名</n-checkbox>
   <template #feedback>切换签名状态后，需以覆盖模式执行关联任务来更新已有 STRM 文件</template>
  </n-form-item>

  <n-form-item v-if="form.EnableStreamSign" label="直链有效期(小时)">
   <n-input-number v-model:value="form.SignExpireHours" :min="0" :max="87600" />
  </n-form-item>

  <n-space justify="end">
    <n-button :disabled="savePending || testPending" @click="closeModal">取消</n-button>
    <n-button :loading="testPending" :disabled="testPending || savePending" @click="testConnection">测试连接</n-button>
    <n-button type="primary" :loading="savePending" :disabled="savePending || testPending" @click="submit">保存</n-button>
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
import { useSecretVisibility } from '../composables/useSecretVisibility'

const message = useMessage()
const dialog = useDialog()
const data = ref([])
const loading = ref(false)
const showModal = ref(false)
const detailPending = ref(false)
const detailPendingId = ref(0)
const savePending = ref(false)
const testPending = ref(false)
const deletingIds = reactive(new Set())
const deleteConfirming = reactive(new Set())
let fetchSeq = 0
let detailSeq = 0
const defaultForm = {
 ID: 0,
 Version: 0,
 Name: '',
 Type: '123pan',
 ClientID: '',
 ClientSecret: '',
 OpenListURL: '',
 OpenListAuthMode: 'password',
 OpenListToken: '',
 OpenListUsername: '',
 OpenListPassword: '',
 WebDAVURL: '',
 WebDAVUsername: '',
 WebDAVPassword: '',
 PlaybackMode: 'redirect',
 EnableStreamSign: false,
 SignExpireHours: 0,
 StrmBaseURL: '',
 CacheTTL: 30,
 CustomCachePolicies: ''
}
const form = reactive({ ...defaultForm })

const typeOptions = [
 { label: '123 云盘开放平台', value: '123pan' },
 { label: 'OpenList (Alist)', value: 'openlist' },
 { label: 'WebDAV', value: 'webdav' }
]
const typeLabels = { '123pan': '123云盘', openlist: 'OpenList', webdav: 'WebDAV' }
const defaultPlaybackMode = (type) => type === 'webdav' ? 'proxy' : 'redirect'
const normalizePlaybackMode = (value, type) => {
 if (value === 'proxy' || value === 'redirect') return value
 return defaultPlaybackMode(type)
}
const playbackMode = (row) => {
 if (normalizePlaybackMode(row.PlaybackMode, row.Type) === 'proxy') return { label: '本机代理', type: 'warning' }
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
   h(NButton, { size: 'tiny', loading: isDetailLoading(row.ID), disabled: detailPending.value || isDeleting(row.ID), onClick: () => openModal(row) }, { default: () => '编辑' }),
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

const {
 visible: secretVisible,
 toggle: toggleSecretVisibility,
 reset: resetSecretVisibility
} = useSecretVisibility(['client_secret', 'openlist_token', 'openlist_password', 'webdav_password'])

const preparePayload = () => {
 const payload = {
  ID: form.ID,
  Version: form.Version,
  Name: form.Name,
  Type: form.Type,
  ClientID: form.ClientID,
  ClientSecret: form.ClientSecret,
  OpenListURL: form.OpenListURL,
  OpenListAuthMode: form.OpenListAuthMode,
  OpenListToken: form.OpenListToken,
  OpenListUsername: form.OpenListUsername,
  OpenListPassword: form.OpenListPassword,
  WebDAVURL: form.Type === 'webdav' ? normalizeUrl(form.WebDAVURL) : form.WebDAVURL,
  WebDAVUsername: form.WebDAVUsername,
  WebDAVPassword: form.WebDAVPassword,
  PlaybackMode: normalizePlaybackMode(form.PlaybackMode, form.Type),
  EnableStreamSign: form.EnableStreamSign,
  SignExpireHours: form.SignExpireHours,
  StrmBaseURL: form.StrmBaseURL,
  CacheTTL: form.CacheTTL,
  CustomCachePolicies: form.CustomCachePolicies
 }
 return payload
}

const clearSecrets = () => {
 form.ClientSecret = ''
 form.OpenListToken = ''
 form.OpenListPassword = ''
 form.WebDAVPassword = ''
 resetSecretVisibility()
}

watch(showModal, (visible) => {
 if (!visible) clearSecrets()
}, { flush: 'sync' })

watch(() => form.Type, (type, previousType) => {
 if (!form.ID && type !== previousType) form.PlaybackMode = defaultPlaybackMode(type)
}, { flush: 'sync' })

const showResponseWarning = (res) => {
 if (typeof res?.warning === 'string' && res.warning.trim()) {
  message.warning(res.warning)
 }
}

const isDetailLoading = (id) => detailPending.value && detailPendingId.value === id

const populateForm = (account) => {
 Object.assign(form, defaultForm, {
  ID: account.ID || 0,
  Version: account.Version || 0,
  Name: account.Name || '',
  Type: account.Type || '123pan',
  ClientID: account.ClientID || '',
  ClientSecret: account.ClientSecret || '',
  OpenListURL: account.OpenListURL || '',
  OpenListAuthMode: account.OpenListAuthMode || 'password',
  OpenListToken: account.OpenListToken || '',
  OpenListUsername: account.OpenListUsername || '',
  OpenListPassword: account.OpenListPassword || '',
  WebDAVURL: account.WebDAVURL || '',
  WebDAVUsername: account.WebDAVUsername || '',
  WebDAVPassword: account.WebDAVPassword || '',
  PlaybackMode: normalizePlaybackMode(account.PlaybackMode, account.Type),
  EnableStreamSign: account.EnableStreamSign === true,
  SignExpireHours: Number.isFinite(account.SignExpireHours) ? account.SignExpireHours : 0,
  StrmBaseURL: account.StrmBaseURL || '',
  CacheTTL: Number.isFinite(account.CacheTTL) ? account.CacheTTL : 30,
  CustomCachePolicies: account.CustomCachePolicies || ''
 })
 resetSecretVisibility()
}

const openModal = async (row) => {
 if (detailPending.value || savePending.value || testPending.value) return
 if (!row) {
  ++detailSeq
  populateForm(defaultForm)
  showModal.value = true
  return
 }

 const requestId = ++detailSeq
 detailPending.value = true
 detailPendingId.value = row.ID
 try {
  const res = await api.get(`/accounts/${row.ID}`)
  if (requestId !== detailSeq) return
  populateForm(res.data || {})
  showModal.value = true
 } catch (e) {
 } finally {
  if (requestId === detailSeq) {
   detailPending.value = false
   detailPendingId.value = 0
  }
 }
}

const closeModal = () => {
 if (savePending.value || testPending.value) return
 showModal.value = false
}

const validateForm = () => {
 if (!form.Name.trim()) {
  message.warning('请输入账户名称')
  return false
 }
 if (form.Type === '123pan') {
  if (!form.ClientID.trim() || !form.ClientSecret.trim()) {
   message.warning('请输入 Client ID 和 Client Secret')
   return false
  }
  return true
 }
 if (form.Type === 'openlist') {
  if (!normalizeUrl(form.OpenListURL)) {
   message.warning('请输入有效的 HTTP(S) OpenList 地址')
   return false
  }
  if (form.OpenListAuthMode === 'token' && !form.OpenListToken.trim()) {
   message.warning('请输入 OpenList Token')
   return false
  }
  if (form.OpenListAuthMode === 'password' && (!form.OpenListUsername.trim() || !form.OpenListPassword.trim())) {
   message.warning('请输入 OpenList 用户名和密码')
   return false
  }
  return true
 }
 if (!normalizeUrl(form.WebDAVURL)) {
  message.warning('请输入有效的 HTTP(S) WebDAV 地址')
  return false
 }
 return true
}

const testConnection = async () => {
 if (testPending.value || savePending.value || !validateForm()) return
 testPending.value = true
 try {
   const res = await api.post('/accounts/test', { ...preparePayload(), ID: 0 })
   message.success(res.message)
  } catch (e) {
 } finally {
  testPending.value = false
 }
}

const submit = async () => {
 if (savePending.value || testPending.value || !validateForm()) return
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
