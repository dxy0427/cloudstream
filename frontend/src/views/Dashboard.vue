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
          <n-tag :type="streamStatus === 'connected' ? 'success' : streamStatus === 'disconnected' ? 'error' : 'warning'" size="small">
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
      <n-empty v-else :description="streamStatus === 'connecting' ? '正在连接实时日志...' : '暂无系统日志'" style="padding: 24px 0;" />
    </n-card>
  </n-space>
</template>

<script setup>
defineOptions({ name: 'Dashboard' })

import { reactive, ref, onMounted, onUnmounted, onActivated, onDeactivated, computed, nextTick, watch } from 'vue'
import api from '../api'

const stats = reactive({ accounts: 0, tasks: 0, enabledTasks: 0 })
const logs = ref([])
const autoScroll = ref(true)
const levelFilter = ref('ALL')
const logContainerRef = ref(null)
const streamStatus = ref('connecting')
const hasInitialized = ref(false)
let eventSource = null
let reconnectTimer = null
let statsTimer = null
let initialLogsTimer = null

const levelOptions = [
  { label: '全部', value: 'ALL' },
  { label: 'INFO', value: 'INFO' },
  { label: 'WARN', value: 'WARN' },
  { label: 'DEBUG', value: 'DEBUG' },
]

const streamStatusText = computed(() => {
  if (streamStatus.value === 'connected') return '实时连接正常'
  if (streamStatus.value === 'reconnecting') return '断线重连中'
  if (streamStatus.value === 'connecting') return '正在连接中'
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
  const res = await api.get('/dashboard/stats')
  const data = res.data || {}
  stats.accounts = data.accounts || 0
  stats.tasks = data.tasks || 0
  stats.enabledTasks = data.enabledTasks || 0
}

const loadInitialLogs = async () => {
  try {
    const res = await api.get('/logs')
    const incoming = Array.isArray(res.data) ? res.data : []
    if (logs.value.length === 0) {
      logs.value = incoming.slice(-100)
    }
    scrollToBottom()
  } catch (e) {
    if (logs.value.length === 0) logs.value = []
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
  const token = localStorage.getItem('jwt_token')
  if (!token) {
    streamStatus.value = 'disconnected'
    return
  }
  eventSource = new EventSource(`/api/v1/logs/stream?token=${encodeURIComponent(token)}`)
  eventSource.onopen = () => {
    streamStatus.value = 'connected'
  }
  eventSource.onmessage = (event) => {
    if (event.data) {
      logs.value.push(event.data)
      if (logs.value.length > 200) logs.value = logs.value.slice(-200)
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

const startStatsRefresh = () => {
  if (statsTimer) return
  statsTimer = setInterval(() => {
    loadStats().catch(() => {})
  }, 10000)
}

const stopStatsRefresh = () => {
  if (statsTimer) {
    clearInterval(statsTimer)
    statsTimer = null
  }
}

onMounted(() => {
  connectLogStream()
  startStatsRefresh()

  if (!hasInitialized.value) {
    loadStats().catch(() => {})
    initialLogsTimer = setTimeout(() => {
      loadInitialLogs().catch(() => {})
      initialLogsTimer = null
    }, 300)
    hasInitialized.value = true
  }
})

onActivated(() => {
  if (!eventSource) connectLogStream()
  loadStats().catch(() => {})
  startStatsRefresh()
  scrollToBottom()
})

onDeactivated(() => {
  if (eventSource) {
    eventSource.close()
    eventSource = null
  }
  if (reconnectTimer) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
  if (initialLogsTimer) {
    clearTimeout(initialLogsTimer)
    initialLogsTimer = null
  }
  stopStatsRefresh()
})

onUnmounted(() => {
  if (eventSource) eventSource.close()
  if (reconnectTimer) clearTimeout(reconnectTimer)
  if (initialLogsTimer) clearTimeout(initialLogsTimer)
  stopStatsRefresh()
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
