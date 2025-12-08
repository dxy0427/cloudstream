<template>
 <n-space vertical>
  <n-card>
   <n-space justify="space-between">
    <h3>任务管理</h3>
    <n-button type="primary" @click="openModal(null)">新建任务</n-button>
   </n-space>
  </n-card>

  <n-data-table :columns="columns" :data="data" :loading="loading" />

  <n-modal v-model:show="showModal" preset="card" title="任务配置" style="width: 700px">
   <n-form label-placement="left" label-width="120">
    <n-form-item label="任务名称">
     <n-input v-model:value="form.Name" />
    </n-form-item>
    <n-form-item label="所属账户">
     <n-select v-model:value="form.AccountID" :options="accountOptions" />
    </n-form-item>
    <n-form-item label="源文件夹ID">
     <n-input-group>
      <n-input v-model:value="form.SourceFolderID" placeholder="123Pan为ID，OpenList为路径" />
      <n-button @click="showBrowser = true">浏览</n-button>
     </n-input-group>
    </n-form-item>
    <n-form-item label="本地路径">
     <n-input v-model:value="form.LocalPath" placeholder="/app/strm/" />
    </n-form-item>
    <n-form-item label="CRON 表达式">
     <n-input v-model:value="form.Cron" placeholder="0 */2 * * *" />
    </n-form-item>
    
    <!-- 新增：自定义后缀配置 -->
    <n-form-item label="STRM 扩展名">
      <n-input v-model:value="form.StrmExtensions" placeholder="mp4,mkv,ts,iso" />
      <template #feedback>
        匹配这些后缀的文件将生成 .strm 播放列表文件
      </template>
    </n-form-item>
    <n-form-item label="元数据 扩展名">
      <n-input v-model:value="form.MetaExtensions" placeholder="jpg,jpeg,png,nfo,srt,ass" />
      <template #feedback>
        匹配这些后缀的文件将直接下载到本地 (如封面、字幕)
      </template>
    </n-form-item>

    <n-form-item label="选项">
     <n-space>
       <n-checkbox v-model:checked="form.Overwrite">覆盖模式</n-checkbox>
       <n-checkbox v-model:checked="form.EncodePath">加密路径</n-checkbox>
     </n-space>
    </n-form-item>
    <n-form-item label="并发线程">
      <n-input-number v-model:value="form.Threads" :min="1" :max="8" />
    </n-form-item>
    <n-space justify="end">
     <n-button type="primary" @click="submit">保存</n-button>
    </n-space>
   </n-form>
  </n-modal>

  <!-- File Browser Modal -->
  <n-modal v-model:show="showBrowser" preset="card" title="选择目录" style="width: 600px; height: 500px">
   <file-browser :account-id="form.AccountID" @select="handleFolderSelect" />
  </n-modal>
 </n-space>
</template>

<script setup>
import { ref, reactive, onMounted, h, onUnmounted } from 'vue'
import { NButton, NSpace, NTag, useMessage, useDialog } from 'naive-ui'
import api from '../api'
import FileBrowser from '../components/FileBrowser.vue'

const message = useMessage()
const dialog = useDialog()
const data = ref([])
const loading = ref(false)
const showModal = ref(false)
const showBrowser = ref(false)
const accountOptions = ref([])

// 默认表单数据
const defaultForm = {
  ID: 0, 
  Name: '', 
  AccountID: null, 
  SourceFolderID: '0', 
  LocalPath: '/app/strm/', 
  Cron: '0 */2 * * *', 
  Overwrite: false, 
  EncodePath: false, 
  Threads: 4,
  StrmExtensions: 'mp4,mkv,ts,iso,mov,avi', 
  MetaExtensions: 'jpg,jpeg,png,nfo,srt,ass,sub'
}

const form = reactive({ ...defaultForm })

const columns = [
 { title: '名称', key: 'Name' },
 { title: '本地路径', key: 'LocalPath' },
 { title: 'CRON', key: 'Cron' },
 { 
  title: '状态', key: 'IsRunning',
  render(row) {
   return row.IsRunning 
    ? h(NTag, { type: 'success' }, { default: () => '运行中' }) 
    : h(NTag, { type: 'default' }, { default: () => '空闲' })
  }
 },
 {
  title: '操作',
  key: 'actions',
  render(row) {
   return h(NSpace, null, {
    default: () => [
     h(NButton, { size: 'small', type: 'info', disabled: row.IsRunning, onClick: () => runTask(row) }, { default: () => '执行' }),
     h(NButton, { size: 'small', type: 'warning', disabled: !row.IsRunning, onClick: () => stopTask(row) }, { default: () => '停止' }),
     h(NButton, { size: 'small', onClick: () => openModal(row) }, { default: () => '编辑' }),
     h(NButton, { size: 'small', type: 'error', onClick: () => handleDelete(row) }, { default: () => '删除' })
    ]
   })
  }
 }
]

const loadData = async () => {
 const [taskRes, accRes] = await Promise.all([
  api.get('/tasks'),
  api.get('/accounts')
 ])
 data.value = taskRes.data || []
 accountOptions.value = (accRes.data || []).map(a => ({ label: a.Name, value: a.ID }))
}

let timer = null
onMounted(() => {
 loadData()
 timer = setInterval(() => api.get('/tasks').then(res => data.value = res.data || []), 5000)
})
onUnmounted(() => clearInterval(timer))

const openModal = (row) => {
 if (row) {
   Object.assign(form, row)
 } else {
   // 重置为默认值
   Object.assign(form, defaultForm)
   // 默认选中第一个账户（如果有）
   if (accountOptions.value.length > 0) {
     form.AccountID = accountOptions.value[0].value
   }
 }
 showModal.value = true
}

const handleFolderSelect = (id) => {
 form.SourceFolderID = id
 showBrowser.value = false
}

const submit = async () => {
 try {
  if (form.ID) await api.put(`/tasks/${form.ID}`, form)
  else await api.post('/tasks', form)
  message.success('保存成功')
  showModal.value = false
  loadData()
 } catch(e) {}
}

const runTask = async (row) => {
 await api.post(`/tasks/${row.ID}/run`)
 message.success('已触发')
 loadData()
}

const stopTask = async (row) => {
 await api.post(`/tasks/${row.ID}/stop`)
 message.success('已发送停止信号')
 loadData()
}

const handleDelete = (row) => {
 dialog.warning({
  title: '警告', content: '删除任务?',
  positiveText: '删除', negativeText: '取消',
  onPositiveClick: async () => {
   await api.delete(`/tasks/${row.ID}`)
   loadData()
  }
 })
}
</script>