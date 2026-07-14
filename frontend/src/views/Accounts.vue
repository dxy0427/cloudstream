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
  <n-data-table :columns="columns" :data="data" :loading="loading" :scroll-x="600" />
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
    <n-input type="password" show-password-on="click" v-model:value="form.ClientSecret" :placeholder="form.ID && form.HasClientSecret ? '已配置，留空不修改' : ''" />
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
         <n-input type="password" show-password-on="click" v-model:value="form.OpenListPassword" :placeholder="form.ID && form.HasOpenListPassword ? '已配置，留空不修改' : 'password'" />
      </n-form-item>
    </n-tab-pane>
    <n-tab-pane name="token" tab="Token">
      <n-form-item label="Token (长期令牌)">
         <n-input type="password" show-password-on="click" v-model:value="form.OpenListToken" :placeholder="form.ID && form.HasOpenListToken ? '已配置，留空不修改' : 'eyJhbGciOi...'" />
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
     <n-input type="password" show-password-on="click" v-model:value="form.WebDAVPassword" :disabled="form.ClearWebDAVPassword" :placeholder="form.ID && form.HasWebDAVPassword ? '已配置，留空不修改' : 'password'" />
   </n-form-item>
   <n-checkbox v-if="form.ID && form.HasWebDAVPassword" v-model:checked="form.ClearWebDAVPassword">清除已保存的 WebDAV 密码</n-checkbox>
   <n-form-item label="播放方式" style="margin-top: 12px;">
    <n-checkbox v-model:checked="form.WebDAVDirectLink">OpenList 302 直链播放</n-checkbox>
    <template #feedback>仅用于 OpenList 的 /dav 地址。开启后 CloudStream 只返回 302，不中转媒体流量；普通 WebDAV 请勿开启。</template>
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
    <n-button :loading="testPending" :disabled="testPending || savePending" @click="testConnection">测试连接</n-button>
    <n-button type="primary" :loading="savePending" :disabled="savePending || testPending" @click="submit">保存</n-button>
  </n-space>
  </n-form>
 </n-modal>
 </n-space>
</template>

<script setup>
import { ref, reactive, onMounted, h, watch } from 'vue'
import { NButton, NSpace, NTag, useMessage, useDialog, NInputNumber } from 'naive-ui'
import api from '../api'

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
    WebDAVURL: '', WebDAVUsername: '', WebDAVPassword: '', WebDAVDirectLink: false, ClearWebDAVPassword: false,
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

const columns = [
 { title: 'ID', key: 'ID', width: 50 },
 { title: '名称', key: 'Name' },
  { title: '类型', key: 'Type', width: 100, render(row) { return h(NTag, { type: row.Type === '123pan' ? 'info' : 'success', size: 'small' }, { default: () => typeLabels[row.Type] || row.Type }) } },
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

const preparePayload = () => ({
 ...form,
 WebDAVURL: form.Type === 'webdav' && form.WebDAVDirectLink ? normalizeUrl(form.WebDAVURL) : form.WebDAVURL
})

const prepareTestPayload = () => {
 const payload = preparePayload()
  if (form.Type === 'webdav' && (form.ClearWebDAVPassword || (!form.WebDAVPassword.trim() && originalBinding && !originalBinding.HasWebDAVPassword))) {
   payload.ID = 0
   payload.WebDAVPassword = ''
  }
 return payload
}

const clearSecrets = () => {
 form.ClientSecret = ''
 form.OpenListToken = ''
 form.OpenListPassword = ''
 form.WebDAVPassword = ''
 form.ClearWebDAVPassword = false
 originalBinding = null
}

watch(() => form.ClearWebDAVPassword, (clear) => {
 if (clear && form.WebDAVDirectLink) form.WebDAVDirectLink = false
})

watch(showModal, (visible) => {
 if (!visible) clearSecrets()
}, { flush: 'sync' })

const showResponseWarning = (res) => {
 if (typeof res?.warning === 'string' && res.warning.trim()) {
  message.warning(res.warning)
 }
}

const openModal = (row) => {
 if (row) {
  Object.assign(form, row, { ClientSecret: '', OpenListToken: '', OpenListPassword: '', WebDAVPassword: '', ClearWebDAVPassword: false })
  originalBinding = captureBinding(row)
 } else {
  Object.assign(form, {
    ID: 0, Name: '', Type: '123pan', 
    ClientID: '', ClientSecret: '', 
     OpenListURL: '', OpenListAuthMode: 'password', OpenListToken: '',
    OpenListUsername: '', OpenListPassword: '',
    WebDAVURL: '', WebDAVUsername: '', WebDAVPassword: '', WebDAVDirectLink: false, ClearWebDAVPassword: false,
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

 if (form.Type === 'webdav' && form.WebDAVDirectLink) {
  const hasPassword = form.WebDAVPassword.trim() || (originalBinding?.HasWebDAVPassword && !form.ClearWebDAVPassword)
  if (!form.WebDAVUsername.trim() || !hasPassword) {
   message.warning('OpenList 302 直链模式需要用户名和密码')
   return false
  }
  try {
	 const parsed = new URL(normalizeUrl(form.WebDAVURL))
	 if (parsed.username || parsed.password || parsed.search || parsed.hash) throw new Error('unsafe URL parts')
	 const pathname = parsed.pathname.replace(/\/+$/, '')
   if (!pathname.toLowerCase().endsWith('/dav')) throw new Error('not OpenList WebDAV')
  } catch (e) {
	 message.warning('OpenList 302 直链模式要求纯 HTTP(S) 地址且路径以 /dav 结尾')
   return false
  }
 }

 if (typeChanged) {
  if (form.Type === '123pan' && (!form.ClientID.trim() || !form.ClientSecret.trim())) {
   message.warning('切换为 123 云盘时必须填写 Client ID 和 Client Secret')
   return false
  }
  if (form.Type === 'openlist') {
   if (!normalizeUrl(form.OpenListURL)) {
    message.warning('切换为 OpenList 时必须填写地址')
    return false
   }
   if (form.OpenListAuthMode === 'token' && !form.OpenListToken.trim()) {
    message.warning('切换为 OpenList Token 认证时必须填写 Token')
    return false
   }
   if (form.OpenListAuthMode === 'password' && (!form.OpenListUsername.trim() || !form.OpenListPassword.trim())) {
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

 if (form.Type === '123pan' && originalBinding.HasClientSecret && !form.ClientSecret.trim() && form.ClientID !== originalBinding.ClientID) {
  message.warning('Client ID 已变更，请重新输入 Client Secret')
  return false
 }
 if (form.Type === 'openlist') {
  const modeChanged = form.OpenListAuthMode !== originalBinding.OpenListAuthMode
  const urlChanged = normalizeUrl(form.OpenListURL) !== originalBinding.OpenListURL
  if (form.OpenListAuthMode === 'token' && !form.OpenListToken.trim() && (modeChanged || (originalBinding.HasOpenListToken && urlChanged))) {
   message.warning('OpenList 地址或认证方式已变更，请重新输入 Token')
   return false
  }
  const usernameChanged = form.OpenListUsername.trim() !== originalBinding.OpenListUsername
  if (form.OpenListAuthMode === 'password' && !form.OpenListPassword.trim() && (modeChanged || (originalBinding.HasOpenListPassword && (urlChanged || usernameChanged)))) {
   message.warning('OpenList 地址、认证方式或用户名已变更，请重新输入密码')
   return false
  }
 }
  if (form.Type === 'webdav' && originalBinding.HasWebDAVPassword && !form.ClearWebDAVPassword && !form.WebDAVPassword.trim()) {
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
 if (testPending.value || savePending.value || !validateSecretBinding()) return
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
 if (savePending.value || testPending.value || !validateSecretBinding()) return
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
