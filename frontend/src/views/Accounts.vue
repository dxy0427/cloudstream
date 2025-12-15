<template>
 <n-space vertical>
 <n-card>
  <n-space justify="space-between" align="center">
  <h3>云账户</h3>
  <n-button type="primary" @click="openModal(null)">添加</n-button>
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
        {{ row.Type === '123pan' ? '123云盘' : 'OpenList' }}
       </n-tag>
       <span style="font-size: 12px; color: #888">缓存: {{ row.CacheTTL }}分</span>
      </template>
      <template #footer>
       <n-space size="small">
        <n-button size="tiny" ghost @click.stop="openModal(row)">编辑</n-button>
        <n-button size="tiny" type="error" ghost @click.stop="handleDelete(row)">删除</n-button>
       </n-space>
      </template>
     </n-thing>
    </n-list-item>
    <n-empty v-if="data.length === 0" description="暂无账户" style="margin-top: 20px" />
   </n-list>
  </n-spin>
 </div>

 <n-modal v-model:show="showModal" preset="card" title="账户配置" style="width: 600px; max-width: 95%">
  <n-form ref="formRef" :model="form" label-placement="top" label-width="auto">
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
   <n-input type="password" show-password-on="click" v-model:value="form.ClientSecret" />
   </n-form-item>
  </template>

  <template v-else>
   <n-form-item label="URL 地址">
   <n-input v-model:value="form.OpenListURL" placeholder="http://192.168.1.5:5244" />
   </n-form-item>
   
   <n-divider dashed>认证方式 (二选一)</n-divider>
   
   <n-tabs type="segment">
    <n-tab-pane name="password" tab="账号密码">
      <n-form-item label="用户名">
        <n-input v-model:value="form.OpenListUsername" placeholder="admin" />
      </n-form-item>
      <n-form-item label="密码">
        <n-input type="password" show-password-on="click" v-model:value="form.OpenListPassword" placeholder="password" />
      </n-form-item>
    </n-tab-pane>
    <n-tab-pane name="token" tab="Token">
      <n-form-item label="Token (长期令牌)">
        <n-input type="password" show-password-on="click" v-model:value="form.OpenListToken" placeholder="eyJhbGciOi..." />
      </n-form-item>
    </n-tab-pane>
   </n-tabs>
  </template>
  
  <n-divider />
  
  <n-form-item label="目录缓存时间 (分钟)">
    <n-input-number v-model:value="form.CacheTTL" :min="0" placeholder="0 表示不缓存" />
    <template #feedback>
      0 表示不缓存；建议设置 1-5 分钟，避免重复扫描或浏览目录时触发风控。
    </template>
  </n-form-item>

  <n-form-item label="STRM Base URL">
   <n-input v-model:value="form.StrmBaseURL" placeholder="http://<IP>:12398" />
  </n-form-item>

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
import { NButton, NSpace, NTag, useMessage, useDialog, NInputNumber } from 'naive-ui'
import api from '../api'

const message = useMessage()
const dialog = useDialog()
const data = ref([])
const loading = ref(false)
const showModal = ref(false)
const form = reactive({ 
    ID: 0, Name: '', Type: '123pan', 
    ClientID: '', ClientSecret: '', 
    OpenListURL: '', OpenListToken: '', 
    OpenListUsername: '', OpenListPassword: '',
    StrmBaseURL: '',
    CacheTTL: 1
})

const typeOptions = [
 { label: '123 云盘开放平台', value: '123pan' },
 { label: 'OpenList (Alist)', value: 'openlist' }
]

const columns = [
 { title: 'ID', key: 'ID', width: 50 },
 { title: '名称', key: 'Name' },
 { title: '类型', key: 'Type', width: 100, render(row) { return h(NTag, { type: row.Type === '123pan' ? 'info' : 'success', size: 'small' }, { default: () => row.Type }) } },
 { title: '缓存', key: 'CacheTTL', width: 80, render(row) { return row.CacheTTL > 0 ? row.CacheTTL + '分' : '无' } },
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
 const res = await api.get('/accounts')
 data.value = res.data || []
 loading.value = false
}

const openModal = (row) => {
 if (row) Object.assign(form, row)
 else Object.assign(form, { 
    ID: 0, Name: '', Type: '123pan', 
    ClientID: '', ClientSecret: '', 
    OpenListURL: '', OpenListToken: '', 
    OpenListUsername: '', OpenListPassword: '',
    StrmBaseURL: '',
    CacheTTL: 1
 })
 showModal.value = true
}

const testConnection = async () => {
 try { const res = await api.post('/accounts/test', form); message.success(res.message) } catch (e) {}
}

const submit = async () => {
 try {
 if (form.ID) await api.put(`/accounts/${form.ID}`, form)
 else await api.post('/accounts', form)
 message.success('保存成功')
 showModal.value = false
 fetchData()
 } catch (e) {}
}

const handleDelete = (row) => {
 dialog.warning({
 title: '警告', content: '删除账户会将关联任务一起删除。', positiveText: '删除', negativeText: '取消',
 onPositiveClick: async () => { await api.delete(`/accounts/${row.ID}`); message.success('删除成功'); fetchData() }
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