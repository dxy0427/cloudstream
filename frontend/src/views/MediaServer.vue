<template>
 <n-space vertical>
 <n-card>
  <n-space justify="space-between" align="center">
  <h3>媒体服务器</h3>
  <n-button type="primary" @click="openModal(null)">添加</n-button>
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
        <n-button size="tiny" ghost @click.stop="openModal(row)">编辑</n-button>
        <n-button size="tiny" type="error" ghost @click.stop="handleDelete(row)">删除</n-button>
       </n-space>
      </template>
     </n-thing>
    </n-list-item>
    <n-empty v-if="data.length === 0" description="暂无媒体服务器" style="margin-top: 20px" />
   </n-list>
  </n-spin>
 </div>

 <n-modal v-model:show="showModal" preset="card" title="媒体服务器配置" style="width: 700px; max-width: 95%">
  <n-form ref="formRef" :model="form" label-placement="top" label-width="auto">
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
    <n-input type="password" show-password-on="click" v-model:value="form.APIKey" :placeholder="form.ID && form.HasAPIKey ? '已配置，留空不修改' : 'Emby/Jellyfin API Key'" />
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
     强制客户端使用直连播放，禁用服务器转码
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
     将原地址替换为新地址，用于内网地址映射
    </template>
   </n-form-item>
  </template>

  <n-divider />

  <n-space justify="end">
   <n-button @click="testConnection">测试连接</n-button>
   <n-button type="primary" @click="submit">保存</n-button>
  </n-space>
  </n-form>
 </n-modal>
 </n-space>
</template>

<script setup>
import { ref, reactive, onMounted, h } from 'vue'
import { NButton, NSpace, NTag, useMessage, useDialog } from 'naive-ui'
import api from '../api'

const message = useMessage()
const dialog = useDialog()
const data = ref([])
const loading = ref(false)
const showModal = ref(false)
const clientListArray = ref([])
const pathMappingsArray = ref([])
const form = reactive({ 
 ID: 0, 
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
	HasAPIKey: false,
 Port: 8091
})

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
   h(NButton, { size: 'tiny', onClick: () => openModal(row) }, { default: () => '编辑' }),
   h(NButton, { size: 'tiny', type: 'error', onClick: () => handleDelete(row) }, { default: () => '删除' })
  ]})
 }
 }
]

const fetchData = async () => {
 loading.value = true
 try {
  const res = await api.get('/mediaservers')
  data.value = res.data || []
 } catch (e) {
  data.value = []
 } finally {
  loading.value = false
 }
}

const openModal = (row) => {
 if (row) Object.assign(form, row)
 else Object.assign(form, { 
  ID: 0, 
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
	  HasAPIKey: false,
  Port: 8091
 })
	form.APIKey = ''
	clientListArray.value = parseArray(form.ClientList)
	pathMappingsArray.value = parseArray(form.PathMappings)
 showModal.value = true
}

const preparePayload = () => ({
	...form,
	ClientList: JSON.stringify(clientListArray.value),
	PathMappings: JSON.stringify(pathMappingsArray.value)
})

const testConnection = async () => {
 try { 
   const res = await api.post('/mediaservers/test', preparePayload())
  message.success(res.message) 
 } catch (e) {}
}

const submit = async () => {
 try {
  const payload = preparePayload()
  if (form.ID) await api.put(`/mediaservers/${form.ID}`, payload)
  else await api.post('/mediaservers', payload)
 message.success('保存成功')
 showModal.value = false
 fetchData()
 } catch (e) {}
}

const handleDelete = (row) => {
 dialog.warning({
 title: '警告', content: '确定要删除此媒体服务器吗？', positiveText: '删除', negativeText: '取消',
 onPositiveClick: async () => {
  try {
   await api.delete(`/mediaservers/${row.ID}`)
   message.success('删除成功')
   fetchData()
  } catch (e) {}
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
