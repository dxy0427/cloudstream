<template>
  <n-space vertical>
    <n-card title="仪表盘">
      <n-space justify="space-around">
        <n-statistic label="云账户总数" :value="stats.accounts" />
        <n-statistic label="任务总数" :value="stats.tasks" />
        <n-statistic label="启用任务" :value="stats.enabledTasks" />
      </n-space>
    </n-card>

    <n-card title="系统日志">
      <n-space justify="space-between" align="center" style="margin-bottom: 12px;">
        <n-space align="center">
          <n-select v-model:value="levelFilter" :options="levelOptions" style="width: 140px;" />
          <n-tag :type="streamStatus === 'connected' ? 'success' : streamStatus === 'reconnecting' ? 'warning' : 'error'" size="small">
            {{ streamStatusText }}
          </n-tag>
        </n-space>
        <n-switch v-model:value="autoScroll">
          <template #checked>自动滚动</template>
          <template #unchecked>自动滚动</template>
        </n-switch>
      </n-space>
      <div ref="logContainerRef" v-if="filteredLogs.length > 0" class="log-panel">
        <div v-for="(line, idx) in filteredLogs" :key="idx" :class="['log-line', levelClass(line)]">{{ line }}</div>
      </div>
      <n-empty v-else description="暂无系统日志" style="padding: 24px 0;" />
    </n-card>
  </n-space>
</template>

<script setup>
import { reactive, ref, onMounted, onUnmounted, computed, nextTick, watch } from 'vue'
import api from '../api'

const stats = reactive({ accounts: 0, tasks: 0, enabledTasks: 0 })
const logs = ref([])
const autoScroll = ref(true)
const levelFilter = ref('ALL')
const logContainerRef = ref(null)
const streamStatus = ref('connecting')
let eventSource = null
let reconnectTimer = null

const levelOptions = [
  { label: '全部', value: 'ALL' },
  { label: 'INFO', value: 'INFO' },
  { label: 'WARN', value: 'WARN' },
  { label: 'DEBUG', value: 'DEBUG' },
]

const streamStatusText = computed(() => {
  if (streamStatus.value === 'connected') return '实时连接正常'
  if (streamStatus.value === 'reconnecting') return '断线重连中'
  return '连接已断开'
})

const filteredLogs = computed(() => {
  if (levelFilter.value === 'ALL') return logs.value
  return logs.value.filter(line => line.includes(`[${levelFilter.value}]`))
})

const scrollToBottom = async () => {
  if (!autoScroll.value) return
  await nextTick()
  const el = logContainerRef.value
  if (el) el.scrollTop = el.scrollHeight
}

watch(autoScroll, (enabled) => {
  if (enabled) scrollToBottom()
})

watch(filteredLogs, () => {
  scrollToBottom()
}, { deep: true })

const levelClass = (line) => {
  if (line.includes('[WARN]')) return 'log-warn'
  if (line.includes('[ERROR]')) return 'log-error'
  if (line.includes('[DEBUG]')) return 'log-debug'
  return 'log-info'
}

const loadStats = async () => {
  const [accRes, taskRes] = await Promise.all([api.get('/accounts'), api.get('/tasks')])
  stats.accounts = (accRes.data || []).length
  stats.tasks = (taskRes.data || []).length
  stats.enabledTasks = (taskRes.data || []).filter(t => t.Enabled).length
}

const loadInitialLogs = async () => {
  try {
    const res = await api.get('/logs')
    logs.value = Array.isArray(res.data) ? res.data : []
    scrollToBottom()
  } catch (e) {
    logs.value = []
  }
}

const scheduleReconnect = () => {
  if (reconnectTimer) return
  streamStatus.value = 'reconnecting'
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null
    connectLogStream()
  }, 3000)
}

const connectLogStream = () => {
  if (eventSource) eventSource.close()
  streamStatus.value = 'connecting'
  eventSource = new EventSource('/api/v1/logs/stream', { withCredentials: true })
  eventSource.onopen = () => {
    streamStatus.value = 'connected'
  }
  eventSource.onmessage = (event) => {
    if (event.data) {
      logs.value.push(event.data)
      if (logs.value.length > 500) logs.value = logs.value.slice(-500)
      scrollToBottom()
    }
  }
  eventSource.onerror = () => {
    if (eventSource) {
      eventSource.close()
      eventSource = null
    }
    scheduleReconnect()
  }
}

onMounted(async () => {
  loadStats()
  await loadInitialLogs()
  connectLogStream()
})

onUnmounted(() => {
  if (eventSource) eventSource.close()
  if (reconnectTimer) clearTimeout(reconnectTimer)
})
</script>

<style scoped>
.log-panel {
  max-height: 400px;
  overflow: auto;
  background: #111;
  color: #ddd;
  padding: 12px;
  border-radius: 8px;
  font-family: monospace;
}
.log-line {
  white-space: pre-wrap;
  line-height: 1.5;
}
.log-info { color: #d4d4d4; }
.log-warn { color: #f7c948; }
.log-error { color: #ff6b6b; }
.log-debug { color: #8b949e; }
</style>
