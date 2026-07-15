<template>
 <n-space vertical>
  <n-card>
   <n-space justify="space-between" align="center">
    <h3>通知管理</h3>
    <n-space>
     <n-button :loading="loading" :disabled="loading || detailPending || savePending || testPending" @click="fetchData">刷新</n-button>
     <n-button type="primary" :disabled="detailPending || savePending || testPending || !loaded" @click="openModal(null)">添加</n-button>
    </n-space>
   </n-space>
  </n-card>

  <n-alert v-if="loadAttempted && !loaded" type="warning">通知列表加载失败，请刷新后重试。</n-alert>

  <div class="desktop-view">
   <n-data-table :columns="columns" :data="data" :loading="loading" :scroll-x="900" />
  </div>

  <div class="mobile-view">
   <n-spin :show="loading">
    <n-list hoverable clickable>
     <n-list-item v-for="row in data" :key="row.ID">
      <n-thing :title="row.Name">
       <template #description>
        <n-space size="small">
         <n-tag :type="row.Type === 'webhook' ? 'info' : 'success'" size="small">{{ typeLabels[row.Type] || row.Type }}</n-tag>
         <n-tag :type="row.Enabled ? 'success' : 'default'" size="small">{{ row.Enabled ? '启用' : '禁用' }}</n-tag>
        </n-space>
        <div class="event-summary">{{ eventSummary(row) }}</div>
       </template>
       <template #footer>
        <n-space size="small">
         <n-button size="tiny" ghost :loading="rowAction(row.ID) === 'test'" :disabled="isRowBusy(row.ID)" @click.stop="testSaved(row)">测试</n-button>
          <n-button size="tiny" ghost :loading="isDetailLoading(row.ID)" :disabled="detailPending || isRowBusy(row.ID)" @click.stop="openModal(row)">编辑</n-button>
         <n-button size="tiny" type="error" ghost :loading="rowAction(row.ID) === 'delete'" :disabled="isRowBusy(row.ID)" @click.stop="handleDelete(row)">删除</n-button>
        </n-space>
       </template>
      </n-thing>
     </n-list-item>
     <n-empty v-if="data.length === 0" description="暂无通知目标" style="margin-top: 20px" />
    </n-list>
   </n-spin>
  </div>

  <n-modal v-model:show="showModal" preset="card" title="通知配置" style="width: 620px; max-width: 95%" :closable="!savePending && !testPending" :mask-closable="!savePending && !testPending" :close-on-esc="!savePending && !testPending">
    <n-form label-placement="top" :disabled="savePending || testPending">
    <n-form-item label="名称">
     <n-input v-model:value="form.Name" maxlength="64" placeholder="例如：运维群通知" />
    </n-form-item>

    <n-form-item label="通知类型">
     <n-select v-model:value="form.Type" :options="typeOptions" />
    </n-form-item>

    <template v-if="form.Type === 'webhook'">
     <n-form-item label="Webhook URL">
        <n-input :type="secretVisible.webhook_url ? 'text' : 'password'" autocomplete="off" v-model:value="form.WebhookURL" placeholder="https://...">
        <template #suffix><n-button text @mousedown.prevent @click.stop="toggleSecretVisibility('webhook_url')"><template #icon><n-icon><EyeInvisibleOutlined v-if="secretVisible.webhook_url" /><EyeOutlined v-else /></n-icon></template></n-button></template>
      </n-input>
     </n-form-item>
    </template>

    <template v-else>
     <n-form-item label="Telegram Bot Token">
        <n-input :type="secretVisible.telegram_token ? 'text' : 'password'" autocomplete="off" v-model:value="form.TelegramToken" placeholder="Bot Token">
        <template #suffix><n-button text @mousedown.prevent @click.stop="toggleSecretVisibility('telegram_token')"><template #icon><n-icon><EyeInvisibleOutlined v-if="secretVisible.telegram_token" /><EyeOutlined v-else /></n-icon></template></n-button></template>
      </n-input>
     </n-form-item>
     <n-form-item label="Telegram Chat ID">
      <n-input v-model:value="form.TelegramChatID" placeholder="Chat ID" />
     </n-form-item>
    </template>

    <n-form-item label="状态">
     <n-checkbox v-model:checked="form.Enabled">启用此通知目标</n-checkbox>
    </n-form-item>

    <n-divider>通知事件</n-divider>
    <n-space vertical>
     <n-checkbox v-model:checked="form.NotifyOnComplete">定时任务完成</n-checkbox>
     <n-checkbox v-model:checked="form.NotifyOnError">任务异常</n-checkbox>
     <n-checkbox v-model:checked="form.NotifyOnStop">手动停止</n-checkbox>
     <n-checkbox v-model:checked="form.NotifyOnManual">手动运行完成</n-checkbox>
    </n-space>

    <n-space justify="end" style="margin-top: 24px">
     <n-button :disabled="savePending || testPending" @click="closeModal">取消</n-button>
     <n-button :loading="testPending" :disabled="savePending || testPending" @click="testDraft">测试通知</n-button>
     <n-button type="primary" :loading="savePending" :disabled="savePending || testPending" @click="submit">保存</n-button>
    </n-space>
   </n-form>
  </n-modal>
 </n-space>
</template>

<script setup>
import { h, onMounted, reactive, ref, watch } from 'vue'
import { NButton, NIcon, NSpace, NTag, useDialog, useMessage } from 'naive-ui'
import api from '../api'
import { EyeInvisibleOutlined, EyeOutlined } from '@vicons/antd'
import { useSecretVisibility } from '../composables/useSecretVisibility'

const message = useMessage()
const dialog = useDialog()
const data = ref([])
const loading = ref(false)
const loaded = ref(false)
const loadAttempted = ref(false)
const showModal = ref(false)
const detailPending = ref(false)
const detailPendingId = ref(0)
const savePending = ref(false)
const testPending = ref(false)
const rowPending = reactive(new Map())
const deleteConfirming = reactive(new Set())
let fetchSeq = 0
let detailSeq = 0

const defaultForm = {
 ID: 0,
 Version: 0,
 Name: '',
 Type: 'webhook',
 WebhookURL: '',
 TelegramToken: '',
 TelegramChatID: '',
 Enabled: true,
 NotifyOnComplete: true,
 NotifyOnError: true,
 NotifyOnStop: true,
 NotifyOnManual: true
}
const form = reactive({ ...defaultForm })

const typeOptions = [
 { label: 'Webhook', value: 'webhook' },
 { label: 'Telegram', value: 'telegram' }
]
const typeLabels = { webhook: 'Webhook', telegram: 'Telegram' }

const eventSummary = (row) => {
 const events = []
 if (row.NotifyOnComplete) events.push('定时完成')
 if (row.NotifyOnError) events.push('异常')
 if (row.NotifyOnStop) events.push('停止')
 if (row.NotifyOnManual) events.push('手动完成')
 return events.length ? events.join('、') : '未选择通知事件'
}

const rowAction = (id) => rowPending.get(id) || ''
const isRowBusy = (id) => !loaded.value || rowPending.has(id) || deleteConfirming.has(id)

const columns = [
 { title: '名称', key: 'Name', width: 160, ellipsis: { tooltip: true } },
 { title: '类型', key: 'Type', width: 100, render(row) { return h(NTag, { type: row.Type === 'webhook' ? 'info' : 'success', size: 'small' }, { default: () => typeLabels[row.Type] || row.Type }) } },
 { title: '状态', key: 'Enabled', width: 80, render(row) { return h(NTag, { type: row.Enabled ? 'success' : 'default', size: 'small' }, { default: () => row.Enabled ? '启用' : '禁用' }) } },
 { title: '通知事件', key: 'events', minWidth: 220, ellipsis: { tooltip: true }, render: eventSummary },
 { title: '操作', key: 'actions', fixed: 'right', width: 190, render(row) { return h(NSpace, { size: 'small' }, { default: () => [
  h(NButton, { size: 'tiny', loading: rowAction(row.ID) === 'test', disabled: isRowBusy(row.ID), onClick: () => testSaved(row) }, { default: () => '测试' }),
  h(NButton, { size: 'tiny', loading: isDetailLoading(row.ID), disabled: detailPending.value || isRowBusy(row.ID), onClick: () => openModal(row) }, { default: () => '编辑' }),
  h(NButton, { size: 'tiny', type: 'error', loading: rowAction(row.ID) === 'delete', disabled: isRowBusy(row.ID), onClick: () => handleDelete(row) }, { default: () => '删除' })
 ] }) } }
]

const fetchData = async () => {
 const requestId = ++fetchSeq
 loading.value = true
 try {
  const res = await api.get('/notifications')
  if (requestId === fetchSeq) {
   data.value = res.data || []
   loaded.value = true
  }
 } catch (e) {
  if (requestId === fetchSeq) loaded.value = false
 } finally {
  if (requestId === fetchSeq) {
   loading.value = false
   loadAttempted.value = true
  }
 }
}

const {
 visible: secretVisible,
 toggle: toggleSecretVisibility,
 reset: resetSecretVisibility
} = useSecretVisibility(['webhook_url', 'telegram_token'])

const clearSecrets = () => {
 form.WebhookURL = ''
 form.TelegramToken = ''
 resetSecretVisibility()
}

watch(showModal, (visible) => {
 if (!visible) clearSecrets()
}, { flush: 'sync' })

const isDetailLoading = (id) => detailPending.value && detailPendingId.value === id

const populateForm = (notification) => {
 Object.assign(form, defaultForm, {
  ID: notification.ID || 0,
  Version: notification.Version || 0,
  Name: notification.Name || '',
  Type: notification.Type || 'webhook',
  WebhookURL: notification.WebhookURL || '',
  TelegramToken: notification.TelegramToken || '',
  TelegramChatID: notification.TelegramChatID || '',
  Enabled: notification.Enabled ?? true,
  NotifyOnComplete: notification.NotifyOnComplete ?? true,
  NotifyOnError: notification.NotifyOnError ?? true,
  NotifyOnStop: notification.NotifyOnStop ?? true,
  NotifyOnManual: notification.NotifyOnManual ?? true
 })
 resetSecretVisibility()
}

const openModal = async (row) => {
 if (detailPending.value || savePending.value || testPending.value) return
 if (!row) {
  ++detailSeq
  populateForm(defaultForm)
  showModal.value = true
  return
 }

 const requestId = ++detailSeq
 detailPending.value = true
 detailPendingId.value = row.ID
 try {
  const res = await api.get(`/notifications/${row.ID}`)
  if (requestId !== detailSeq) return
  populateForm(res.data || {})
  showModal.value = true
 } catch (e) {
 } finally {
  if (requestId === detailSeq) {
   detailPending.value = false
   detailPendingId.value = 0
  }
 }
}

const closeModal = () => {
 if (savePending.value || testPending.value) return
 showModal.value = false
}

const validateForm = () => {
 if (!form.Name.trim()) {
  message.warning('请输入通知名称')
  return false
 }
 if (form.Type === 'webhook') {
  if (!form.WebhookURL.trim()) {
   message.warning('请输入 Webhook URL')
   return false
  }
  return true
 }
 if (!form.TelegramToken.trim()) {
  message.warning('请输入 Telegram Bot Token')
  return false
 }
 if (!form.TelegramChatID.trim()) {
  message.warning('请输入 Telegram Chat ID')
  return false
 }
 return true
}

const buildPayload = () => ({
 ID: form.ID,
 Version: form.Version,
 Name: form.Name.trim(),
 Type: form.Type,
 WebhookURL: form.WebhookURL.trim(),
 TelegramToken: form.TelegramToken.trim(),
 TelegramChatID: form.TelegramChatID.trim(),
 Enabled: form.Enabled,
 NotifyOnComplete: form.NotifyOnComplete,
 NotifyOnError: form.NotifyOnError,
 NotifyOnStop: form.NotifyOnStop,
 NotifyOnManual: form.NotifyOnManual
})

const testDraft = async () => {
 if (testPending.value || savePending.value || !validateForm()) return
 testPending.value = true
 try {
  await api.post('/notifications/test', { ...buildPayload(), ID: 0 })
  message.success('测试通知发送成功')
 } catch (e) {
 } finally {
  testPending.value = false
 }
}

const testSaved = async (row) => {
 if (isRowBusy(row.ID)) return
 rowPending.set(row.ID, 'test')
 try {
  await api.post('/notifications/test', { ID: row.ID, Version: row.Version })
  message.success('测试通知发送成功')
 } catch (e) {
 } finally {
  rowPending.delete(row.ID)
 }
}

const submit = async () => {
 if (savePending.value || testPending.value || !validateForm()) return
 savePending.value = true
 try {
  const payload = buildPayload()
  const res = form.ID ? await api.put(`/notifications/${form.ID}`, payload) : await api.post('/notifications', payload)
  message.success('通知配置已保存')
  showModal.value = false
  clearSecrets()
  const saved = res.data
  if (saved?.ID) {
   const index = data.value.findIndex(item => item.ID === saved.ID)
   if (index >= 0) data.value.splice(index, 1, saved)
   else data.value.push(saved)
  }
  fetchData().catch(() => {})
 } catch (e) {
 } finally {
  savePending.value = false
 }
}

const handleDelete = (row) => {
 if (isRowBusy(row.ID)) return
 deleteConfirming.add(row.ID)
 dialog.warning({
  title: '删除通知目标', content: `确定删除“${row.Name}”吗？`, positiveText: '删除', negativeText: '取消', closable: false, maskClosable: false, closeOnEsc: false,
  onNegativeClick: () => deleteConfirming.delete(row.ID),
  onPositiveClick: async () => {
   deleteConfirming.delete(row.ID)
   if (rowPending.has(row.ID)) return
   rowPending.set(row.ID, 'delete')
   try {
    await api.delete(`/notifications/${row.ID}`, { params: { version: row.Version } })
    message.success('通知目标已删除')
    data.value = data.value.filter(item => item.ID !== row.ID)
    fetchData().catch(() => {})
   } catch (e) {
   } finally {
    rowPending.delete(row.ID)
   }
  }
 })
}

onMounted(fetchData)
</script>

<style scoped>
.mobile-view { display: none; }
.desktop-view { display: block; }
.event-summary { margin-top: 8px; color: #888; font-size: 12px; }
@media (max-width: 600px) {
 .desktop-view { display: none; }
 .mobile-view { display: block; }
}
</style>
