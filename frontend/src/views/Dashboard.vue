<template>
  <n-space vertical>
    <!-- 响应式布局，强制等高 -->
    <n-grid x-gap="12" y-gap="12" cols="1 s:3" responsive="screen">
      <n-gi>
        <n-card title="云账户" style="height: 100%">
          <n-statistic label="已配置" :value="stats.accountCount">
            <template #prefix><n-icon><CloudOutlined /></n-icon></template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi>
        <n-card title="任务总数" style="height: 100%">
          <n-statistic label="自动扫描" :value="stats.taskCount">
            <template #prefix><n-icon><UnorderedListOutlined /></n-icon></template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi>
        <n-card title="运行中" style="height: 100%">
          <n-statistic label="正在执行" :value="stats.runningCount">
            <template #prefix><n-icon><SyncOutlined spin /></n-icon></template>
          </n-statistic>
        </n-card>
      </n-gi>
    </n-grid>

    <n-card title="系统日志" size="small">
      <template #header-extra>
        <n-space align="center">
          <n-switch v-model:value="autoRefresh" size="small">
            <template #checked>自动刷新</template>
            <template #unchecked>暂停刷新</template>
          </n-switch>
          <n-button size="tiny" @click="fetchLogs">手动刷新</n-button>
        </n-space>
      </template>
      <div 
        ref="logContainer"
        class="log-viewer"
        v-html="formattedLogs"
      >
      </div>
    </n-card>
  </n-space>
</template>

<script setup>
import { ref, onMounted, onUnmounted, watch, nextTick, computed } from 'vue'
import { CloudOutlined, UnorderedListOutlined, SyncOutlined } from '@vicons/antd'
import { NIcon } from 'naive-ui'
import api from '../api'

const stats = ref({ accountCount: 0, taskCount: 0, runningCount: 0 })
const rawLogs = ref('加载中...')
const autoRefresh = ref(true)
const logContainer = ref(null)
let logTimer = null

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
    const newLogs = res.data || '暂无日志'
    
    if (newLogs !== rawLogs.value) {
      rawLogs.value = newLogs
      nextTick(() => {
        if (logContainer.value) {
          logContainer.value.scrollTop = logContainer.value.scrollHeight
        }
      })
    }
  } catch (e) {
    if (rawLogs.value === '加载中...') rawLogs.value = '获取日志失败'
  }
}

// 日志格式化：添加颜色高亮
const formattedLogs = computed(() => {
  if (!rawLogs.value) return ''
  return rawLogs.value.split('\n').map(line => {
    if (!line.trim()) return ''
    let color = '#ddd' // 默认颜色
    if (line.includes('"level":"info"')) color = '#4caf50' // 绿色
    else if (line.includes('"level":"warn"')) color = '#ff9800' // 橙色
    else if (line.includes('"level":"error"')) color = '#f44336' // 红色
    else if (line.includes('"level":"fatal"')) color = '#ff0000' // 亮红

    // 尝试解析 JSON 日志简化显示，或者直接高亮原始文本
    // 这里简单做个关键词高亮
    let content = line
      .replace(/"level":"info"/g, '<span style="color:#4caf50;font-weight:bold">[INFO]</span>')
      .replace(/"level":"warn"/g, '<span style="color:#ff9800;font-weight:bold">[WARN]</span>')
      .replace(/"level":"error"/g, '<span style="color:#f44336;font-weight:bold">[ERROR]</span>')
      .replace(/"message"/g, '<span style="color:#888">msg</span>')
    
    return `<div style="border-bottom: 1px solid #333; padding: 2px 0;">${content}</div>`
  }).join('')
})

const startTimer = () => {
  stopTimer()
  fetchLogs()
  logTimer = setInterval(fetchLogs, 3000)
}

const stopTimer = () => {
  if (logTimer) {
    clearInterval(logTimer)
    logTimer = null
  }
}

watch(autoRefresh, (val) => {
  if (val) startTimer()
  else stopTimer()
})

onMounted(() => {
  fetchData()
  startTimer()
})

onUnmounted(() => {
  stopTimer()
})
</script>

<style scoped>
.log-viewer {
  background-color: #1e1e1e;
  padding: 10px;
  border-radius: 4px;
  height: 350px;
  overflow-y: auto;
  font-family: 'Fira Code', monospace;
  font-size: 12px;
  color: #ddd;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>