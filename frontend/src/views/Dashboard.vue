<template>
 <n-space vertical>
  <n-grid x-gap="12" :cols="3">
   <n-gi>
    <n-card title="云账户">
     <n-statistic label="已配置" :value="stats.accountCount">
      <template #prefix><n-icon><CloudOutlined /></n-icon></template>
     </n-statistic>
    </n-card>
   </n-gi>
   <n-gi>
    <n-card title="任务总数">
     <n-statistic label="自动扫描" :value="stats.taskCount">
      <template #prefix><n-icon><UnorderedListOutlined /></n-icon></template>
     </n-statistic>
    </n-card>
   </n-gi>
   <n-gi>
    <n-card title="运行中">
     <n-statistic label="正在执行" :value="stats.runningCount">
      <template #prefix><n-icon><SyncOutlined spin /></n-icon></template>
     </n-statistic>
    </n-card>
   </n-gi>
  </n-grid>

  <n-card title="系统日志" size="small">
    <template #header-extra>
      <n-button size="tiny" @click="fetchLogs">刷新日志</n-button>
    </template>
    <n-log
      :log="logs"
      language="text"
      :rows="15"
      style="background-color: #1e1e1e; padding: 10px; border-radius: 4px; color: #ddd;"
    />
  </n-card>
 </n-space>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { CloudOutlined, UnorderedListOutlined, SyncOutlined } from '@vicons/antd'
import { NIcon, NLog } from 'naive-ui'
import api from '../api'

const stats = ref({ accountCount: 0, taskCount: 0, runningCount: 0 })
const logs = ref('加载中...')

const fetchData = async () => {
 const [accRes, taskRes] = await Promise.all([
  api.get('/accounts'),
  api.get('/tasks')
 ])
 stats.value.accountCount = accRes.data ? accRes.data.length : 0
 if (taskRes.data) {
  stats.value.taskCount = taskRes.data.length
  stats.value.runningCount = taskRes.data.filter(t => t.IsRunning).length
 }
}

const fetchLogs = async () => {
  try {
    const res = await api.get('/logs')
    logs.value = res.data || '暂无日志'
  } catch (e) {
    logs.value = '获取日志失败'
  }
}

onMounted(() => {
  fetchData()
  fetchLogs()
})
</script>