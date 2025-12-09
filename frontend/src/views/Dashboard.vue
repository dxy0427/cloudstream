<template>
  <n-space vertical>
    <!-- 响应式布局 -->
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
      <!-- 日志容器 -->
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
    
    // 只有日志内容变化时才更新，防止滚动条跳动
    if (newLogs !== rawLogs.value) {
      rawLogs.value = newLogs
      // 数据更新后，等待 DOM 渲染，然后滚动到底部
      nextTick(() => {
        if (logContainer.value && autoRefresh.value) {
          logContainer.value.scrollTop = logContainer.value.scrollHeight
        }
      })
    }
  } catch (e) {
    if (rawLogs.value === '加载中...') rawLogs.value = '获取日志失败'
  }
}

// 简单的日志着色器
const formattedLogs = computed(() => {
  if (!rawLogs.value) return ''
  return rawLogs.value.split('\n').map(line => {
    if (!line.trim()) return ''
    
    // JSON 格式日志优化显示
    let content = line
    // 将日志级别替换为带颜色的标签
    content = content
      .replace(/"level":"info"/g, '<span style="color:#52c41a;font-weight:bold">[INFO]</span>')
      .replace(/"level":"warn"/g, '<span style="color:#faad14;font-weight:bold">[WARN]</span>')
      .replace(/"level":"error"/g, '<span style="color:#f5222d;font-weight:bold">[ERROR]</span>')
      .replace(/"level":"fatal"/g, '<span style="color:#f5222d;font-weight:bold;background:#330000">[FATAL]</span>')
      
      // 弱化 key 显示
      .replace(/"time":/g, '<span style="color:#666">time:</span>')
      .replace(/"message":/g, '<span style="color:#666">msg:</span>')
      .replace(/"task":/g, '<span style="color:#1890ff">task:</span>')

    return `<div style="border-bottom: 1px solid #333333; padding: 4px 0; line-height: 1.5;">${content}</div>`
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
  padding: 12px;
  border-radius: 4px;
  height: 400px;
  overflow-y: auto;
  font-family: 'Fira Code', 'Consolas', monospace;
  font-size: 13px;
  color: #e0e0e0;
  white-space: pre-wrap;
  word-break: break-all;
  /* 平滑滚动 */
  scroll-behavior: smooth;
}

/* 自定义滚动条 */
.log-viewer::-webkit-scrollbar {
  width: 8px;
}
.log-viewer::-webkit-scrollbar-track {
  background: #2b2b2b;
}
.log-viewer::-webkit-scrollbar-thumb {
  background: #555;
  border-radius: 4px;
}
.log-viewer::-webkit-scrollbar-thumb:hover {
  background: #777;
}
</style>