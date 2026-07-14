<template>
  <n-space vertical>
    <n-card>
      <n-space justify="space-between" align="center">
        <h3>任务管理</h3>
        <n-button type="primary" :disabled="savePending" @click="openModal(null)">新建</n-button>
      </n-space>
    </n-card>

    <div class="desktop-view">
      <n-data-table :columns="columns" :data="data" :loading="loading" :scroll-x="1200" />
    </div>

    <div class="mobile-view">
      <n-spin :show="loading">
        <n-list hoverable clickable>
          <n-list-item v-for="row in data" :key="row.ID">
            <template #prefix>
              <n-tag :type="row.IsRunning ? 'success' : 'default'" size="small">{{ row.IsRunning ? '运行' : '空闲' }}</n-tag>
            </template>
            <n-thing :title="row.Name">
              <template #description>
                <div>{{ row.LocalPath }}</div>
                <div style="color: #888; font-size: 12px; margin-top: 4px;">已处理: {{ row.ProcessedCount }} | 状态: {{ row.LastRunStatus || '无' }}</div>
              </template>
              <template #footer>
                <n-space size="small" style="margin-top: 5px">
                  <n-button size="tiny" type="info" ghost :loading="rowAction(row.ID) === 'run'" :disabled="row.IsRunning || isRowBusy(row.ID)" @click.stop="runTask(row)">执行</n-button>
                  <n-button size="tiny" type="warning" ghost :loading="rowAction(row.ID) === 'stop'" :disabled="!row.IsRunning || isRowBusy(row.ID)" @click.stop="stopTask(row)">停止</n-button>
                  <n-button size="tiny" ghost :disabled="isRowBusy(row.ID)" @click.stop="openModal(row)">编辑</n-button>
                  <n-button size="tiny" type="error" ghost :loading="rowAction(row.ID) === 'delete'" :disabled="isRowBusy(row.ID)" @click.stop="handleDelete(row)">删除</n-button>
                </n-space>
              </template>
            </n-thing>
          </n-list-item>
          <n-empty v-if="data.length === 0" description="暂无任务" style="margin-top: 20px" />
        </n-list>
      </n-spin>
    </div>

    <n-modal v-model:show="showModal" preset="card" title="任务配置" style="width: 700px; max-width: 95%;" :closable="!savePending" :mask-closable="!savePending" :close-on-esc="!savePending">
      <n-form label-placement="top" label-width="auto" :disabled="savePending">
        <n-form-item label="任务名称"><n-input v-model:value="form.Name" /></n-form-item>
        <n-form-item label="所属账户"><n-select v-model:value="form.AccountID" :options="accountOptions" /></n-form-item>
        <n-form-item label="源文件夹ID">
          <n-input-group>
            <n-input v-model:value="form.SourceFolderID" placeholder="123Pan为ID，OpenList为路径" />
            <n-button @click="showBrowser = true">浏览</n-button>
          </n-input-group>
        </n-form-item>
        <n-form-item label="本地路径"><n-input v-model:value="form.LocalPath" placeholder="/app/strm/" /></n-form-item>
        <n-form-item label="CRON 表达式"><n-input v-model:value="form.Cron" placeholder="0 */2 * * *" /></n-form-item>
        <n-form-item label="STRM 扩展名"><n-input v-model:value="form.StrmExtensions" placeholder="mp4,mkv,ts,iso" /></n-form-item>
        <n-form-item label="元数据 扩展名"><n-input v-model:value="form.MetaExtensions" placeholder="jpg,jpeg,png,nfo" /></n-form-item>
        <n-form-item label="选项">
          <n-space vertical>
            <n-checkbox v-model:checked="form.Enabled">启用定时任务</n-checkbox>
            <n-checkbox v-model:checked="form.Overwrite">覆盖模式</n-checkbox>
            <n-checkbox v-model:checked="form.SyncDelete">同步删除</n-checkbox>
            <n-checkbox v-model:checked="form.EncodePath">签名</n-checkbox>
          </n-space>
        </n-form-item>
        <n-form-item label="直链有效期(小时)">
          <n-input-number v-model:value="form.SignExpireHours" :min="0" :max="87600" style="width: 200px" />
        </n-form-item>
        <n-form-item label="并发线程"><n-input-number v-model:value="form.Threads" :min="1" :max="16" /></n-form-item>
        <n-space justify="end">
          <n-button :disabled="savePending" @click="showModal = false">取消</n-button>
          <n-button type="primary" :loading="savePending" :disabled="savePending" @click="submit">保存</n-button>
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
const loading = ref(true)
const showModal = ref(false)
const showBrowser = ref(false)
const accountOptions = ref([])
const savePending = ref(false)
const rowPending = reactive(new Map())
const deleteConfirming = reactive(new Set())
let eventSource = null
let reconnectTimer = null
let reconnectNeedsAuth = false
let httpRequestSeq = 0
let streamSnapshotSeq = 0
let currentStreamHasSnapshot = false
let unmounted = false
let mutationRefreshActive = false

const defaultForm = {
  ID: 0, Name: '', AccountID: null, SourceFolderID: '0', LocalPath: '/app/strm/', Cron: '0 */2 * * *', Enabled: true, Overwrite: false, SyncDelete: false, EncodePath: false, SignExpireHours: 0, Threads: 4,
  StrmExtensions: 'mp4,mkv,ts,iso,mov,avi', MetaExtensions: 'jpg,jpeg,png,nfo,srt,ass,sub'
}
const form = reactive({ ...defaultForm })

const columns = [
  { title: '名称', key: 'Name', fixed: 'left', width: 120, ellipsis: { tooltip: true } },
  { title: '路径', key: 'LocalPath', width: 150, ellipsis: { tooltip: true } },
  { title: '执行情况', key: 'ProcessedCount', width: 200, render(row) { return h('div', [h('div', { style: 'font-size: 12px; color: #888' }, `状态: ${row.LastRunStatus || '未执行'}`), h('div', `已处理: ${row.ProcessedCount} 个文件`)]) } },
  { title: 'CRON', key: 'Cron', width: 100 },
  { title: '状态', key: 'IsRunning', width: 80, render(row) { return h(NTag, { type: row.IsRunning ? 'success' : 'default', size: 'small' }, { default: () => row.IsRunning ? '运行' : '空闲' }) } },
  { title: '操作', key: 'actions', fixed: 'right', width: 180, render(row) { return h(NSpace, { size: 'small' }, { default: () => [h(NButton, { size: 'tiny', type: 'info', loading: rowAction(row.ID) === 'run', disabled: row.IsRunning || isRowBusy(row.ID), onClick: () => runTask(row) }, { default: () => '执行' }), h(NButton, { size: 'tiny', type: 'warning', loading: rowAction(row.ID) === 'stop', disabled: !row.IsRunning || isRowBusy(row.ID), onClick: () => stopTask(row) }, { default: () => '停止' }), h(NButton, { size: 'tiny', disabled: isRowBusy(row.ID), onClick: () => openModal(row) }, { default: () => '编辑' }), h(NButton, { size: 'tiny', type: 'error', loading: rowAction(row.ID) === 'delete', disabled: isRowBusy(row.ID), onClick: () => handleDelete(row) }, { default: () => '删除' })] }) } }
]

const loadAccounts = async () => {
  try {
    const accRes = await api.get('/accounts')
    accountOptions.value = (accRes.data || []).map(a => ({ label: a.Name, value: a.ID }))
  } catch (e) {
    // 错误已由 api 拦截器全局弹出提示
  }
}

const showResponseWarning = (res) => {
  if (typeof res?.warning === 'string' && res.warning.trim()) {
    message.warning(res.warning)
  }
}

const loadTasksFromHttp = async ({ showLoading = false, authoritative = false } = {}) => {
  if (unmounted) return false
  const requestId = ++httpRequestSeq
  const snapshotAtStart = streamSnapshotSeq
  if (showLoading && data.value.length === 0) loading.value = true
  try {
    const res = await api.get('/tasks')
    if (unmounted || requestId !== httpRequestSeq || (!authoritative && snapshotAtStart !== streamSnapshotSeq)) return false
    data.value = Array.isArray(res.data) ? res.data : []
    return true
  } catch (e) {
    // 保留已有 SSE/HTTP 快照
    return false
  } finally {
    if (!unmounted && requestId === httpRequestSeq && (authoritative || snapshotAtStart === streamSnapshotSeq)) {
      loading.value = false
    }
  }
}

const closeTaskStream = () => {
  const source = eventSource
  eventSource = null
  currentStreamHasSnapshot = false
  if (source) source.close()
}

const refreshTasksAfterMutation = async () => {
	mutationRefreshActive = true
	closeTaskStream()
  if (reconnectTimer) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
	try {
		await loadTasksFromHttp({ authoritative: true })
	} finally {
		mutationRefreshActive = false
	}
	if (!unmounted && document.visibilityState === 'visible') connectTaskStream()
}

const scheduleReconnect = (requireAuth = false) => {
  reconnectNeedsAuth = reconnectNeedsAuth || requireAuth
  if (reconnectTimer || unmounted || document.visibilityState !== 'visible') return
  reconnectTimer = setTimeout(async () => {
    reconnectTimer = null
		if (unmounted || mutationRefreshActive || document.visibilityState !== 'visible' || eventSource) return
    const shouldProbeAuth = reconnectNeedsAuth
    reconnectNeedsAuth = false
    if (shouldProbeAuth) {
      try {
        await api.get('/username', { skipErrorToast: true })
      } catch (error) {
        if (error?.response?.status !== 401) scheduleReconnect(true)
        return
      }
    }
		if (!mutationRefreshActive) connectTaskStream()
  }, 3000)
}

const connectTaskStream = () => {
	if (eventSource || mutationRefreshActive || unmounted || document.visibilityState !== 'visible') return
  // EventSource 同源请求自动携带 HTTP-only Cookie，无需手动传 token
  const source = new EventSource('/api/v1/tasks/stream')
  eventSource = source
  source.onopen = () => {
    if (eventSource !== source) return
    reconnectNeedsAuth = false
  }
  source.addEventListener('tasks', (event) => {
    if (eventSource !== source) return
    try {
      const snapshot = JSON.parse(event.data)
      if (!Array.isArray(snapshot)) throw new Error('invalid tasks snapshot')
      currentStreamHasSnapshot = true
      streamSnapshotSeq++
      httpRequestSeq++
      data.value = snapshot
      loading.value = false
    } catch (e) {
      if (!currentStreamHasSnapshot) {
        loading.value = false
        loadTasksFromHttp().catch(() => {})
      }
    }
  })
  source.onerror = () => {
    if (eventSource !== source) return
    const closed = source.readyState === EventSource.CLOSED
    closeTaskStream()
    loading.value = false
    loadTasksFromHttp().catch(() => {})
    if (document.visibilityState === 'visible') {
      scheduleReconnect(closed)
    } else if (closed) {
      reconnectNeedsAuth = true
    }
  }
}

const handleVisibilityChange = () => {
  if (document.visibilityState !== 'visible') {
    closeTaskStream()
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
    return
  }
  if (reconnectNeedsAuth) scheduleReconnect(true)
  else connectTaskStream()
  loadTasksFromHttp().catch(() => {})
}

onMounted(() => {
  loadAccounts()
  connectTaskStream()
  loadTasksFromHttp({ showLoading: true }).catch(() => {})
  document.addEventListener('visibilitychange', handleVisibilityChange)
})

onUnmounted(() => {
  unmounted = true
  httpRequestSeq++
  closeTaskStream()
  if (reconnectTimer) clearTimeout(reconnectTimer)
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})

const rowAction = (id) => rowPending.get(id)
const isRowBusy = (id) => rowPending.has(id) || deleteConfirming.has(id)

const beginRowAction = (id, action) => {
  if (isRowBusy(id)) return false
  rowPending.set(id, action)
  return true
}

const openModal = (row) => {
  Object.assign(form, defaultForm)
  if (row) {
    for (const key of Object.keys(defaultForm)) {
      if (row[key] !== undefined) form[key] = row[key]
    }
  } else {
    if (accountOptions.value.length > 0) form.AccountID = accountOptions.value[0].value
  }
  showModal.value = true
}

const handleFolderSelect = (id) => {
  form.SourceFolderID = id
  showBrowser.value = false
}

const submit = async () => {
  if (savePending.value) return
  if (!form.Name || !form.AccountID || !form.SourceFolderID || !form.LocalPath || !form.Cron) {
    message.warning('请填写完整的任务信息')
    return
  }
  if (!form.LocalPath.startsWith('/')) {
    message.warning('本地路径必须是绝对路径')
    return
  }
  savePending.value = true
  try {
    const payload = Object.fromEntries(Object.keys(defaultForm).map(key => [key, form[key]]))
    const res = form.ID
      ? await api.put(`/tasks/${form.ID}`, payload)
      : await api.post('/tasks', payload)
    message.success('保存成功')
    showResponseWarning(res)
    showModal.value = false
    await refreshTasksAfterMutation()
  } catch (e) {
  } finally {
    savePending.value = false
  }
}

const runTask = async (row) => {
  if (!beginRowAction(row.ID, 'run')) return
  try {
    const res = await api.post(`/tasks/${row.ID}/run`)
    message.success('已触发')
    showResponseWarning(res)
    await refreshTasksAfterMutation()
  } catch (e) {
  } finally {
    rowPending.delete(row.ID)
  }
}
const stopTask = async (row) => {
  if (!beginRowAction(row.ID, 'stop')) return
  try {
    const res = await api.post(`/tasks/${row.ID}/stop`)
    message.success('已发送停止信号')
    showResponseWarning(res)
    await refreshTasksAfterMutation()
  } catch (e) {
  } finally {
    rowPending.delete(row.ID)
  }
}
const handleDelete = (row) => {
  if (isRowBusy(row.ID)) return
  deleteConfirming.add(row.ID)
  dialog.warning({
    title: '警告', content: '删除任务？', positiveText: '删除', negativeText: '取消', closable: false, maskClosable: false, closeOnEsc: false,
    onNegativeClick: () => deleteConfirming.delete(row.ID),
    onPositiveClick: async () => {
      deleteConfirming.delete(row.ID)
      if (!beginRowAction(row.ID, 'delete')) return
      try {
        const res = await api.delete(`/tasks/${row.ID}`)
        message.success('删除成功')
        showResponseWarning(res)
        await refreshTasksAfterMutation()
      } catch (e) {
      } finally {
        rowPending.delete(row.ID)
      }
    }
  })
}
</script>

<style scoped>
.mobile-view { display: none; }
.desktop-view { display: block; }
@media (max-width: 600px) {
  .desktop-view { display: none; }
  .mobile-view { display: block; }
}
</style>
