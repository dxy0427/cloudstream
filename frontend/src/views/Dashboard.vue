<template>
  <n-space vertical>
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
      <div ref="logContainer" class="log-viewer" v-html="formattedLogs"></div>
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
  const [accRes, taskRes] = await Promise.all([api.get('/accounts'), api.get('/tasks')])
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
        if (logContainer.value && autoRefresh.value) logContainer.value.scrollTop = logContainer.value.scrollHeight
      })
    }
  } catch (e) {
    if (rawLogs.value === '加载中...') rawLogs.value = '获取日志失败'
  }
}

const escapeHtml = (str) => {
  if (!str) return ''
  return str.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/'/g, "&#039;");
}

const formattedLogs = computed(() => {
  if (!rawLogs.value) return ''
  return rawLogs.value.split('\n').map(line => {
    if (!line.trim()) return ''
    let content = escapeHtml(line)
    content = content
      .replace(/&quot;level&quot;:&quot;info&quot;/g, '<span style="color:#52c41a;font-weight:bold">[INFO]</span>')
      .replace(/&quot;level&quot;:&quot;warn&quot;/g, '<span style="color:#faad14;font-weight:bold">[WARN]</span>')
      .replace(/&quot;level&quot;:&quot;error&quot;/g, '<span style="color:#f5222d;font-weight:bold">[ERROR]</span>')
      .replace(/&quot;level&quot;:&quot;fatal&quot;/g, '<span style="color:#f5222d;font-weight:bold;background:#330000">[FATAL]</span>')
      .replace(/&quot;time&quot;:/g, '<span style="color:#666">time:</span>')
      .replace(/&quot;msg&quot;:/g, '<span style="color:#666">msg:</span>')
      .replace(/&quot;message&quot;:/g, '<span style="color:#666">msg:</span>')
      .replace(/&quot;task&quot;:/g, '<span style="color:#1890ff">task:</span>')
    return `<div style="border-bottom: 1px solid #333333; padding: 4px 0; line-height: 1.5;">${content}</div>`
  }).join('')
})

const startTimer = () => { stopTimer(); fetchLogs(); logTimer = setInterval(fetchLogs, 3000) }
const stopTimer = () => { if (logTimer) { clearInterval(logTimer); logTimer = null } }
watch(autoRefresh, (val) => { if (val) startTimer(); else stopTimer() })
onMounted(() => { fetchData(); startTimer() })
onUnmounted(() => { stopTimer() })
</script>

<style scoped>
.log-viewer { background-color: #1e1e1e; padding: 12px; border-radius: 4px; height: 400px; overflow-y: auto; font-family: 'Fira Code', monospace; font-size: 13px; color: #e0e0e0; white-space: pre-wrap; word-break: break-all; scroll-behavior: smooth; }
.log-viewer::-webkit-scrollbar { width: 8px; }
.log-viewer::-webkit-scrollbar-track { background: #2b2b2b; }
.log-viewer::-webkit-scrollbar-thumb { background: #555; border-radius: 4px; }
</style>