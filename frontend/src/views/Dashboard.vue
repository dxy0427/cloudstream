<template>
  <n-space vertical>
    <n-card title="仪表盘">
      <n-space justify="space-around">
        <n-statistic label="云账户总数" :value="stats.accounts" />
        <n-statistic label="任务总数" :value="stats.tasks" />
        <n-statistic label="运行中任务" :value="stats.runningTasks" />
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
        <n-switch v-model:value="autoScroll" :disabled="!canRenderLogs">
          <template #checked>自动滚动</template>
          <template #unchecked>自动滚动</template>
        </n-switch>
      </n-space>

      <template v-if="canRenderLogs">
        <div ref="logContainerRef" v-if="displayLogs.length > 0" class="log-panel">
          <div v-for="(line, idx) in displayLogs" :key="idx" :class="['log-line', levelClass(line)]">{{ line }}</div>
        </div>
        <n-empty v-else description="暂无系统日志" style="padding: 24px 0;" />
      </template>
      <n-empty v-else description="正在连接实时日志..." style="padding: 24px 0;" />
    </n-card>
  </n-space>
</template>

<script setup>
defineOptions({ name: 'Dashboard' })

import { reactive, ref, onMounted, onUnmounted, onActivated, onDeactivated, computed, nextTick, watch } from 'vue'
import api from '../api'

const stats = reactive({ accounts: 0, tasks: 0, runningTasks: 0 })
const logs = ref([])
const autoScroll = ref(true)
const levelFilter = ref('ALL')
const logContainerRef = ref(null)
const streamStatus = ref('connecting')
const hasInitialized = ref(false)
const hasEverConnected = ref(false)
let eventSource = null
let reconnectTimer = null
let statsTimer = null
let resumeTimer = null
let scrollTimer = null
let scrollRafId = null
let isActive = false
let clearLogsOnOpen = false
let streamGeneration = 0

const levelOptions = [
  { label: '全部', value: 'ALL' },
  { label: 'INFO', value: 'INFO' },
  { label: 'WARN', value: 'WARN' },
  { label: 'ERROR', value: 'ERROR' },
  { label: 'DEBUG', value: 'DEBUG' },
]

const streamStatusText = computed(() => {
  if (streamStatus.value === 'connected') return '实时连接正常'
  if (streamStatus.value === 'reconnecting') return '断线重连中'
  if (streamStatus.value === 'connecting') return '正在连接中'
  return '连接已断开'
})

const canRenderLogs = computed(() => streamStatus.value === 'connected' || hasEverConnected.value)

const filteredLogs = computed(() => {
  if (!canRenderLogs.value) return []
  if (levelFilter.value === 'ALL') return logs.value
  return logs.value.filter(line => line.includes(`[${levelFilter.value}]`))
})

const displayLogs = computed(() => filteredLogs.value.slice(-200))

const shouldAutoScroll = () => canRenderLogs.value && autoScroll.value

const scrollToBottomNow = () => {
  if (!shouldAutoScroll()) return
  const el = logContainerRef.value
  if (el) el.scrollTop = el.scrollHeight
}

const cancelPendingScroll = () => {
  if (scrollTimer) {
    clearTimeout(scrollTimer)
    scrollTimer = null
  }
  if (scrollRafId !== null) {
    cancelAnimationFrame(scrollRafId)
    scrollRafId = null
  }
}

const ensureScrollToBottom = async () => {
  if (!shouldAutoScroll()) return
  cancelPendingScroll()
  await nextTick()
  if (!shouldAutoScroll()) return
  scrollToBottomNow()
  scrollRafId = requestAnimationFrame(() => {
    scrollRafId = null
    if (!shouldAutoScroll()) return
    scrollToBottomNow()
  })
  scrollTimer = setTimeout(() => {
    scrollTimer = null
    if (!shouldAutoScroll()) return
    scrollToBottomNow()
  }, 60)
}

watch(autoScroll, (enabled) => {
  if (!enabled) {
    cancelPendingScroll()
    return
  }
  if (canRenderLogs.value) ensureScrollToBottom()
})

watch(displayLogs, () => {
  if (canRenderLogs.value) ensureScrollToBottom()
}, { deep: true })

watch(canRenderLogs, (enabled) => {
  if (enabled) ensureScrollToBottom()
})

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
  stats.runningTasks = data.runningTasks || 0
}

const scheduleReconnect = () => {
  if (!isActive || eventSource || reconnectTimer || document.visibilityState !== 'visible') return
  streamStatus.value = 'reconnecting'
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null
    if (isActive && document.visibilityState === 'visible') connectLogStream()
  }, 3000)
}

const closeLogStream = () => {
  const source = eventSource
  eventSource = null
  if (source) source.close()
}

const connectLogStream = () => {
  if (!isActive || eventSource || document.visibilityState !== 'visible') return
  streamStatus.value = 'connecting'
  // 后端每次连接都会先推送历史日志，成功建立后再清空旧快照。
  clearLogsOnOpen = true
  // EventSource 同源请求自动携带 HTTP-only Cookie，无需手动传 token
  const source = new EventSource('/api/v1/logs/stream')
  const generation = ++streamGeneration
  eventSource = source
  source.onopen = () => {
    if (!isActive || eventSource !== source) return
    if (clearLogsOnOpen) {
      logs.value = []
      clearLogsOnOpen = false
    }
    streamStatus.value = 'connected'
    hasEverConnected.value = true
    ensureScrollToBottom()
  }
  source.onmessage = (event) => {
    if (!isActive || eventSource !== source) return
    if (event.data) {
      logs.value.push(event.data)
      if (logs.value.length > 200) logs.value = logs.value.slice(-200)
      if (canRenderLogs.value) ensureScrollToBottom()
    }
  }
  source.onerror = () => {
    if (eventSource !== source) return
    // EventSource 在 401 时 readyState 会进入 CLOSED；停止无意义重连并走登录
    const closed = source.readyState === EventSource.CLOSED
    closeLogStream()
		streamStatus.value = closed ? 'reconnecting' : 'disconnected'
    if (!isActive || document.visibilityState !== 'visible') {
      streamStatus.value = 'disconnected'
      return
    }
    if (closed) {
      // 探测会话是否仍有效；只有有效时才重连
      api.get('/username', { skipErrorToast: true })
        .then(() => {
          if (!eventSource && generation === streamGeneration) scheduleReconnect()
        })
        .catch((error) => {
          if (!eventSource && generation === streamGeneration && error?.response?.status !== 401) scheduleReconnect()
        })
      return
    }
    scheduleReconnect()
  }
}

const handleVisibilityChange = () => {
  if (!isActive) return
  if (document.visibilityState !== 'visible') {
    closeLogStream()
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
    streamStatus.value = 'disconnected'
    return
  }
  if (!eventSource) connectLogStream()
  loadStats().catch(() => {})
}

const startStatsRefresh = () => {
  if (statsTimer) return
  statsTimer = setInterval(() => {
    loadStats().catch(() => {})
  }, 30000)
}

const stopStatsRefresh = () => {
  if (statsTimer) {
    clearInterval(statsTimer)
    statsTimer = null
  }
}

onMounted(() => {
  isActive = true
  if (!hasInitialized.value) {
    loadStats().catch(() => {})
    connectLogStream()
    startStatsRefresh()
    document.addEventListener('visibilitychange', handleVisibilityChange)
    hasInitialized.value = true
  }
})

onActivated(() => {
  isActive = true
  if (!statsTimer) startStatsRefresh()
  if (autoScroll.value && canRenderLogs.value) ensureScrollToBottom()
  if (resumeTimer) clearTimeout(resumeTimer)
  resumeTimer = setTimeout(() => {
    if (!isActive) return
    if (!eventSource) connectLogStream()
    loadStats().catch(() => {})
    if (autoScroll.value && canRenderLogs.value) ensureScrollToBottom()
    resumeTimer = null
  }, 120)
})

onDeactivated(() => {
  isActive = false
  stopStatsRefresh()
  closeLogStream()
  streamStatus.value = 'disconnected'
  if (reconnectTimer) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
  if (resumeTimer) {
    clearTimeout(resumeTimer)
    resumeTimer = null
  }
  cancelPendingScroll()
})

onUnmounted(() => {
  isActive = false
  closeLogStream()
  if (reconnectTimer) clearTimeout(reconnectTimer)
  if (resumeTimer) clearTimeout(resumeTimer)
  document.removeEventListener('visibilitychange', handleVisibilityChange)
  cancelPendingScroll()
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
