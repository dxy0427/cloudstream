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
        <n-select v-model:value="levelFilter" :options="levelOptions" style="width: 140px;" />
        <n-switch v-model:value="autoScroll">
          <template #checked>自动滚动</template>
          <template #unchecked>自动滚动</template>
        </n-switch>
      </n-space>
      <div ref="logContainerRef" v-if="filteredLogs.length > 0" style="max-height: 400px; overflow: auto; background: #111; color: #ddd; padding: 12px; border-radius: 8px; font-family: monospace; white-space: pre-wrap;">{{ filteredLogs.join('\n') }}</div>
      <n-empty v-else description="暂无系统日志" style="padding: 24px 0;" />
    </n-card>
  </n-space>
</template>

<script setup>
import { reactive, ref, onMounted, onUnmounted, computed, nextTick } from 'vue'
import api from '../api'

const stats = reactive({ accounts: 0, tasks: 0, enabledTasks: 0 })
const logs = ref([])
const autoScroll = ref(true)
const levelFilter = ref('ALL')
const logContainerRef = ref(null)
let eventSource = null
let reconnectTimer = null

const levelOptions = [
  { label: '全部', value: 'ALL' },
  { label: 'INFO', value: 'INFO' },
  { label: 'WARN', value: 'WARN' },
  { label: 'DEBUG', value: 'DEBUG' },
]

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

const loadStats = async () => {
  const [accRes, taskRes] = await Promise.all([api.get('/accounts'), api.get('/tasks')])
  stats.accounts = (accRes.data || []).length
  stats.tasks = (taskRes.data || []).length
  stats.enabledTasks = (taskRes.data || []).filter(t => t.Enabled).length
}

const scheduleReconnect = () => {
  if (reconnectTimer) return
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null
    connectLogStream()
  }, 3000)
}

const connectLogStream = () => {
  const token = localStorage.getItem('jwt_token')
  if (!token) return
  if (eventSource) eventSource.close()

  eventSource = new EventSource(`/api/v1/logs/stream?token=${encodeURIComponent(token)}`)
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

onMounted(() => {
  loadStats()
  connectLogStream()
})

onUnmounted(() => {
  if (eventSource) eventSource.close()
  if (reconnectTimer) clearTimeout(reconnectTimer)
})
</script>
