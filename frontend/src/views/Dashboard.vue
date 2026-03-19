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
      <n-space justify="end" style="margin-bottom: 12px;">
        <n-button size="small" @click="loadLogs" :loading="logsLoading">刷新</n-button>
      </n-space>
      <n-spin :show="logsLoading">
        <div v-if="logs.length > 0" style="max-height: 400px; overflow: auto; background: #111; color: #0f0; padding: 12px; border-radius: 8px; font-family: monospace; white-space: pre-wrap;">{{ logs.join('\n') }}</div>
        <n-empty v-else description="暂无系统日志" style="padding: 24px 0;" />
      </n-spin>
    </n-card>
  </n-space>
</template>

<script setup>
import { reactive, ref, onMounted } from 'vue'
import api from '../api'

const stats = reactive({ accounts: 0, tasks: 0, enabledTasks: 0 })
const logs = ref([])
const logsLoading = ref(false)

const loadStats = async () => {
  const [accRes, taskRes] = await Promise.all([api.get('/accounts'), api.get('/tasks')])
  stats.accounts = (accRes.data || []).length
  stats.tasks = (taskRes.data || []).length
  stats.enabledTasks = (taskRes.data || []).filter(t => t.Enabled).length
}

const loadLogs = async () => {
  logsLoading.value = true
  try {
    const res = await api.get('/logs')
    const data = res.data
    if (Array.isArray(data)) {
      logs.value = data.filter(Boolean)
    } else if (typeof data === 'string' && data.trim()) {
      logs.value = data.split('\n').filter(Boolean)
    } else {
      logs.value = []
    }
  } catch (e) {
    logs.value = []
  } finally {
    logsLoading.value = false
  }
}

onMounted(() => {
  loadStats()
  loadLogs()
})
</script>
