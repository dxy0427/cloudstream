<template>
 <n-space vertical>
 <n-card>
  <n-space justify="space-between" align="center">
  <h3>媒体服务器</h3>
  <n-button type="primary" :disabled="detailPending || savePending || testPending" @click="openModal(null)">添加</n-button>
  </n-space>
 </n-card>

 <!-- 桌面表格 -->
 <div class="desktop-view">
  <n-data-table :columns="columns" :data="data" :loading="loading" :scroll-x="800" />
 </div>

 <!-- 移动端列表 -->
 <div class="mobile-view">
  <n-spin :show="loading">
   <n-list hoverable clickable>
    <n-list-item v-for="row in data" :key="row.ID">
     <n-thing :title="row.Name">
      <template #description>
       <n-tag :type="row.ServerType === 'Emby' ? 'info' : 'success'" size="small" style="margin-right: 5px">
        {{ row.ServerType }}
       </n-tag>
       <n-tag :type="row.Enabled ? 'success' : 'default'" size="small" style="margin-right: 5px">
        {{ row.Enabled ? '启用' : '禁用' }}
       </n-tag>
       <span style="font-size: 12px; color: #888">{{ row.ServerAddr }}</span>
      </template>
      <template #footer>
       <n-space size="small">
          <n-button size="tiny" ghost :loading="isDetailLoading(row.ID)" :disabled="detailPending || isDeleting(row.ID)" @click.stop="openModal(row)">编辑</n-button>
         <n-button size="tiny" type="error" ghost :loading="isDeleting(row.ID)" :disabled="isDeleting(row.ID)" @click.stop="handleDelete(row)">删除</n-button>
       </n-space>
      </template>
     </n-thing>
    </n-list-item>
    <n-empty v-if="data.length === 0" description="暂无媒体服务器" style="margin-top: 20px" />
   </n-list>
  </n-spin>
 </div>

 <n-modal v-model:show="showModal" preset="card" title="媒体服务器配置" style="width: 700px; max-width: 95%" :closable="!savePending && !testPending" :mask-closable="!savePending && !testPending" :close-on-esc="!savePending && !testPending">
   <n-form ref="formRef" :model="form" label-placement="top" label-width="auto" :disabled="savePending || testPending">
  <n-form-item label="名称" path="Name">
   <n-input v-model:value="form.Name" placeholder="服务器备注名称" />
  </n-form-item>

  <n-form-item label="服务器类型" path="ServerType">
   <n-select v-model:value="form.ServerType" :options="serverTypeOptions" />
  </n-form-item>

  <n-form-item label="服务器地址" path="ServerAddr">
   <n-input v-model:value="form.ServerAddr" placeholder="http://192.168.1.5:8096" />
  </n-form-item>

  <n-form-item label="API Key" path="APIKey">
      <n-input :type="apiKeyVisibility.api_key ? 'text' : 'password'" autocomplete="off" v-model:value="form.APIKey" placeholder="Emby/Jellyfin API Key">
       <template #suffix><n-button text @mousedown.prevent @click.stop="toggleApiKeyVisibility('api_key')"><template #icon><n-icon><EyeInvisibleOutlined v-if="apiKeyVisibility.api_key" /><EyeOutlined v-else /></n-icon></template></n-button></template>
     </n-input>
  </n-form-item>

  <n-divider />

  <n-form-item label="启用状态" path="Enabled">
   <n-switch v-model:value="form.Enabled" />
  </n-form-item>

  <n-divider title-placement="left">缓存配置</n-divider>

  <n-form-item label="启用缓存" path="CacheEnable">
   <n-switch v-model:value="form.CacheEnable" />
  </n-form-item>

  <n-form-item label="HTTPStrm缓存TTL (分钟)" path="HttpStrmTTL">
   <n-input-number v-model:value="form.HttpStrmTTL" :min="1" :max="1440" />
  </n-form-item>

  <n-divider title-placement="left">客户端过滤</n-divider>

  <n-form-item label="启用客户端过滤" path="ClientEnable">
   <n-switch v-model:value="form.ClientEnable" />
  </n-form-item>

  <template v-if="form.ClientEnable">
   <n-form-item label="过滤模式" path="ClientMode">
    <n-select v-model:value="form.ClientMode" :options="clientModeOptions" />
   </n-form-item>

   <n-form-item label="客户端列表" path="ClientList">
    <n-dynamic-tags v-model:value="clientListArray" />
    <template #feedback>
     输入客户端UA关键词，如：Infuse、Fileball、Emby等
    </template>
   </n-form-item>
  </template>

  <n-divider title-placement="left">HTTPStrm配置</n-divider>

  <n-form-item label="启用HTTPStrm" path="HttpStrmEnable">
   <n-switch v-model:value="form.HttpStrmEnable" />
  </n-form-item>

  <template v-if="form.HttpStrmEnable">
   <n-form-item label="禁用转码" path="DisableTranscode">
    <n-switch v-model:value="form.DisableTranscode" />
    <template #feedback>
     仅禁止 HTTPStrm 通过媒体服务器转码，普通媒体不受影响
    </template>
   </n-form-item>

   <n-form-item label="解析Strm链接" path="ResolveStrmLinks">
    <n-switch v-model:value="form.ResolveStrmLinks" />
    <template #feedback>
     自动解析.strm文件中的HTTP直链
    </template>
   </n-form-item>

   <n-form-item label="UA透传" path="UaPassthrough">
    <n-switch v-model:value="form.UaPassthrough" />
    <template #feedback>
     使用客户端UA请求直链
    </template>
   </n-form-item>

   <n-form-item label="路径映射" path="PathMappings">
    <n-dynamic-input v-model:value="pathMappingsArray" :on-create="createPathMapping">
     <template #default="{ value }">
      <n-input v-model:value="value.old" placeholder="原地址" style="width: 45%" />
      <span style="padding: 0 8px">→</span>
      <n-input v-model:value="value.new" placeholder="新地址" style="width: 45%" />
     </template>
    </n-dynamic-input>
    <template #feedback>
     填写完整的 HTTP/HTTPS 地址前缀；空白行会被忽略
    </template>
   </n-form-item>
  </template>

  <n-divider />

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
import { NButton, NIcon, NSpace, NTag, useMessage, useDialog } from 'naive-ui'
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
const clientListArray = ref([])
const pathMappingsArray = ref([])
let fetchSeq = 0
let detailSeq = 0
const defaultForm = {
 ID: 0, 
 Version: 0,
 Name: '', 
 ServerType: 'Emby', 
 ServerAddr: '', 
 APIKey: '',
 Enabled: true,
 CacheEnable: true,
 HttpStrmTTL: 1,
 ClientEnable: false,
 ClientMode: 'BlackList',
 ClientList: '[]',
 HttpStrmEnable: true,
 DisableTranscode: true,
 ResolveStrmLinks: true,
 UaPassthrough: false,
 PathMappings: '[]',
 Port: 8091
}
const form = reactive({ ...defaultForm })

const serverTypeOptions = [
 { label: 'Emby', value: 'Emby' },
 { label: 'Jellyfin', value: 'Jellyfin' }
]

const clientModeOptions = [
 { label: '白名单', value: 'WhiteList' },
 { label: '黑名单', value: 'BlackList' }
]

const parseArray = (value) => {
 try {
  const parsed = JSON.parse(value || '[]')
  return Array.isArray(parsed) ? parsed : []
 } catch {
  return []
 }
}

const createPathMapping = () => ({ old: '', new: '' })

const columns = [
 { title: 'ID', key: 'ID', width: 50 },
 { title: '名称', key: 'Name' },
 { title: '类型', key: 'ServerType', width: 100, render(row) { return h(NTag, { type: row.ServerType === 'Emby' ? 'info' : 'success', size: 'small' }, { default: () => row.ServerType }) } },
 { title: '地址', key: 'ServerAddr', ellipsis: { tooltip: true } },
 { title: '状态', key: 'Enabled', width: 80, render(row) { return h(NTag, { type: row.Enabled ? 'success' : 'default', size: 'small' }, { default: () => row.Enabled ? '启用' : '禁用' }) } },
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
  const res = await api.get('/mediaservers')
  if (requestId === fetchSeq) data.value = res.data || []
 } catch (e) {
 } finally {
  if (requestId === fetchSeq) loading.value = false
 }
}

const {
 visible: apiKeyVisibility,
 toggle: toggleApiKeyVisibility,
 reset: resetApiKeyVisibility
} = useSecretVisibility(['api_key'])

const clearApiKey = () => {
 form.APIKey = ''
 resetApiKeyVisibility()
}

watch(showModal, (visible) => {
 if (!visible) clearApiKey()
}, { flush: 'sync' })

const showResponseWarning = (res) => {
 if (typeof res?.warning === 'string' && res.warning.trim()) {
  message.warning(res.warning)
 }
}

const isDetailLoading = (id) => detailPending.value && detailPendingId.value === id

const populateForm = (server) => {
 Object.assign(form, defaultForm, {
  ID: server.ID || 0,
  Version: server.Version || 0,
  Name: server.Name || '',
  ServerType: server.ServerType || 'Emby',
  ServerAddr: server.ServerAddr || '',
  APIKey: server.APIKey || '',
  Enabled: server.Enabled ?? true,
  CacheEnable: server.CacheEnable ?? true,
  HttpStrmTTL: Number.isFinite(server.HttpStrmTTL) ? server.HttpStrmTTL : 1,
  ClientEnable: server.ClientEnable ?? false,
  ClientMode: server.ClientMode || 'BlackList',
  ClientList: server.ClientList || '[]',
  HttpStrmEnable: server.HttpStrmEnable ?? true,
  DisableTranscode: server.DisableTranscode ?? true,
  ResolveStrmLinks: server.ResolveStrmLinks ?? true,
  UaPassthrough: server.UaPassthrough ?? false,
  PathMappings: server.PathMappings || '[]',
  Port: Number.isFinite(server.Port) ? server.Port : 8091
 })
 clientListArray.value = parseArray(form.ClientList)
 pathMappingsArray.value = parseArray(form.PathMappings)
 resetApiKeyVisibility()
}

const preparePayload = () => ({
 ID: form.ID,
 Version: form.Version,
 Name: form.Name,
 ServerType: form.ServerType,
 ServerAddr: form.ServerAddr,
 APIKey: form.APIKey,
 Enabled: form.Enabled,
 CacheEnable: form.CacheEnable,
 HttpStrmTTL: form.HttpStrmTTL,
 ClientEnable: form.ClientEnable,
 ClientMode: form.ClientMode,
 ClientList: JSON.stringify(clientListArray.value),
 HttpStrmEnable: form.HttpStrmEnable,
 DisableTranscode: form.DisableTranscode,
 ResolveStrmLinks: form.ResolveStrmLinks,
 UaPassthrough: form.UaPassthrough,
 PathMappings: JSON.stringify(pathMappingsArray.value.filter(({ old, new: replacement }) => old.trim() || replacement.trim())),
 Port: form.Port
})

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
  const res = await api.get(`/mediaservers/${row.ID}`)
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
  message.warning('请输入服务器名称')
  return false
 }
 if (!form.ServerAddr.trim()) {
  message.warning('请输入服务器地址')
  return false
 }
 if (!form.APIKey.trim()) {
  message.warning('请输入 API Key')
  return false
 }
 const invalidMapping = pathMappingsArray.value.some(({ old, new: replacement }) => {
  const source = old.trim()
  const target = replacement.trim()
  if (!source && !target) return false
 try {
   return [source, target].some((value) => {
    const parsed = new URL(value)
    return !['http:', 'https:'].includes(parsed.protocol) || parsed.username || parsed.password || parsed.search || parsed.hash
   })
  } catch {
   return true
  }
 })
 if (invalidMapping) {
  message.warning('路径映射必须填写完整的 HTTP/HTTPS 地址前缀')
  return false
 }
 return true
}

const testConnection = async () => {
 if (testPending.value || savePending.value || !validateForm()) return
 testPending.value = true
 try { 
   const res = await api.post('/mediaservers/test', { ...preparePayload(), ID: 0 })
  message.success(res.message) 
  showResponseWarning(res)
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
   ? await api.put(`/mediaservers/${form.ID}`, payload)
   : await api.post('/mediaservers', payload)
  message.success('保存成功')
  showResponseWarning(res)
  showModal.value = false
  clearApiKey()
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
 title: '警告', content: '确定要删除此媒体服务器吗？', positiveText: '删除', negativeText: '取消', closable: false, maskClosable: false, closeOnEsc: false,
 onNegativeClick: () => deleteConfirming.delete(row.ID),
 onPositiveClick: async () => {
  deleteConfirming.delete(row.ID)
  if (deletingIds.has(row.ID)) return
  deletingIds.add(row.ID)
  try {
   const res = await api.delete(`/mediaservers/${row.ID}`)
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
