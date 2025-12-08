<template>
 <n-space vertical>
  <n-card>
   <n-space justify="space-between" align="center">
    <h3>任务管理</h3>
    <n-button type="primary" @click="openModal(null)">新建</n-button>
   </n-space>
  </n-card>

  <!-- 桌面端显示表格 -->
  <div class="desktop-view">
    <n-data-table :columns="columns" :data="data" :loading="loading" :scroll-x="1000" />
  </div>

  <!-- 移动端显示列表卡片 -->
  <div class="mobile-view">
    <n-spin :show="loading">
      <n-list hoverable clickable>
        <n-list-item v-for="row in data" :key="row.ID">
          <template #prefix>
            <n-tag :type="row.IsRunning ? 'success' : 'default'" size="small">
              {{ row.IsRunning ? '运行' : '空闲' }}
            </n-tag>
          </template>
          <n-thing :title="row.Name" :description="row.LocalPath">
            <template #footer>
              <n-space size="small" style="margin-top: 5px">
                <n-button size="tiny" type="info" ghost @click.stop="runTask(row)" :disabled="row.IsRunning">执行</n-button>
                <n-button size="tiny" type="warning" ghost @click.stop="stopTask(row)" :disabled="!row.IsRunning">停止</n-button>
                <n-button size="tiny" ghost @click.stop="openModal(row)">编辑</n-button>
                <n-button size="tiny" type="error" ghost @click.stop="handleDelete(row)">删</n-button>
              </n-space>
            </template>
          </n-thing>
        </n-list-item>
        <n-empty v-if="data.length === 0" description="暂无任务" style="margin-top: 20px" />
      </n-list>
    </n-spin>
  </div>

  <n-modal v-model:show="showModal" preset="card" title="任务配置" style="width: 700px; max-width: 95%;">
   <n-form label-placement="top" label-width="auto">
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
    
    <n-form-item label="STRM 扩展名">
      <n-input v-model:value="form.StrmExtensions" placeholder="mp4,mkv,ts,iso" />
    </n-form-item>
    <n-form-item label="元数据 扩展名">
      <n-input v-model:value="form.MetaExtensions" placeholder="jpg,jpeg,png,nfo" />
    </n-form-item>

    <n-form-item label="选项">
     <n-space vertical>
       <n-checkbox v-model:checked="form.Overwrite">覆盖模式</n-checkbox>
       <n-checkbox v-model:checked="form.SyncDelete">同步删除</n-checkbox>
       <n-checkbox v-model:checked="form.EncodePath">加密路径</n-checkbox>
     </n-space>
    </n-form-item>
    
    <n-form-item label="并发线程">
      <n-input-number v-model:value="form.Threads" :min="1" :max="8" />
    </n-form-item>
    <n-space justify="end">
     <n-button @click="showModal = false">取消</n-button>
     <n-button type="primary" @click="submit">保存</n-button>
    </n-space>
   </n-form>
  </n-modal>

  <n-modal v-model:show="showBrowser" preset="card" title="选择目录" style="width: 600px; height: 80vh; max-width: 95%">
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

const defaultForm = {
  ID: 0, Name: '', AccountID: null, SourceFolderID: '0', LocalPath: '/app/strm/', Cron: '0 */2 * * *', Overwrite: false, SyncDelete: false, EncodePath: false, Threads: 4,
  StrmExtensions: 'mp4,mkv,ts,iso,mov,avi', MetaExtensions: 'jpg,jpeg,png,nfo,srt,ass,sub'
}
const form = reactive({ ...defaultForm })

const columns = [
 { title: '名称', key: 'Name', fixed: 'left', width: 120, ellipsis: { tooltip: true } },
 { title: '路径', key: 'LocalPath', width: 150, ellipsis: { tooltip: true } },
 { title: 'CRON', key: 'Cron', width: 100 },
 { 
  title: '状态', key: 'IsRunning', width: 80,
  render(row) {
   return h(NTag, { type: row.IsRunning ? 'success' : 'default', size: 'small' }, { default: () => row.IsRunning ? '运行' : '空闲' })
  }
 },
 {
  title: '操作', key: 'actions', fixed: 'right', width: 180,
  render(row) {
   return h(NSpace, { size: 'small' }, {
    default: () => [
     h(NButton, { size: 'tiny', type: 'info', disabled: row.IsRunning, onClick: () => runTask(row) }, { default: () => '跑' }),
     h(NButton, { size: 'tiny', type: 'warning', disabled: !row.IsRunning, onClick: () => stopTask(row) }, { default: () => '停' }),
     h(NButton, { size: 'tiny', onClick: () => openModal(row) }, { default: () => '改' }),
     h(NButton, { size: 'tiny', type: 'error', onClick: () => handleDelete(row) }, { default: () => '删' })
    ]
   })
  }
 }
]

const loadData = async () => {
 const [taskRes, accRes] = await Promise.all([api.get('/tasks'), api.get('/accounts')])
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
 if (row) Object.assign(form, row)
 else {
   Object.assign(form, defaultForm)
   if (accountOptions.value.length > 0) form.AccountID = accountOptions.value[0].value
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

const runTask = async (row) => { await api.post(`/tasks/${row.ID}/run`); message.success('已触发'); loadData() }
const stopTask = async (row) => { await api.post(`/tasks/${row.ID}/stop`); message.success('已发送停止信号'); loadData() }
const handleDelete = (row) => {
 dialog.warning({
  title: '警告', content: '删除任务？', positiveText: '删除', negativeText: '取消',
  onPositiveClick: async () => { await api.delete(`/tasks/${row.ID}`); loadData() }
 })
}
</script>

<style scoped>
/* 桌面端默认显示表格，隐藏列表 */
.mobile-view { display: none; }
.desktop-view { display: block; }

/* 移动端显示列表，隐藏表格 */
@media (max-width: 600px) {
  .desktop-view { display: none; }
  .mobile-view { display: block; }
}
</style>