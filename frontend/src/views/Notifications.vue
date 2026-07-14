<template>
  <n-space vertical>
    <n-card title="通知管理">
      <n-alert v-if="!loaded" type="warning" style="margin-bottom: 16px;">
        通知设置尚未加载，重新加载成功前不会允许保存。
        <template #action>
          <n-button size="small" :loading="pending === 'load'" :disabled="pending !== null" @click="reload">重试</n-button>
        </template>
      </n-alert>
      <n-form label-placement="top" :disabled="pending !== null || !loaded">
        <n-form-item label="通知类型">
          <n-radio-group v-model:value="form.notifyType">
            <n-space>
              <n-radio value="webhook">Webhook</n-radio>
              <n-radio value="telegram">Telegram</n-radio>
            </n-space>
          </n-radio-group>
        </n-form-item>

        <template v-if="form.notifyType === 'webhook'">
          <n-form-item label="Webhook URL">
             <n-input v-model:value="form.webhookUrl" :disabled="form.clearWebhookUrl || pending !== null" :placeholder="stored.hasWebhookUrl ? '已配置，留空不修改' : '请输入机器人 Webhook 地址'" />
          </n-form-item>
        </template>

        <template v-else>
          <n-form-item label="Telegram Bot Token">
             <n-input type="password" show-password-on="click" v-model:value="form.telegramToken" :disabled="form.clearTelegramToken || pending !== null" :placeholder="stored.hasTelegramToken ? '已配置，留空不修改' : '请输入 Bot Token'" />
          </n-form-item>
          <n-form-item label="Telegram Chat ID">
            <n-input v-model:value="form.telegramChatId" :disabled="pending !== null" placeholder="请输入 Chat ID" />
          </n-form-item>
        </template>

        <template v-if="stored.hasWebhookUrl || stored.hasTelegramToken">
          <n-divider>清理已保存凭据</n-divider>
          <n-space vertical>
            <n-checkbox v-if="stored.hasWebhookUrl" v-model:checked="form.clearWebhookUrl" :disabled="pending !== null">
              清除已保存的 Webhook URL
            </n-checkbox>
            <n-checkbox v-if="stored.hasTelegramToken" v-model:checked="form.clearTelegramToken" :disabled="pending !== null">
              清除已保存的 Telegram Bot Token
            </n-checkbox>
          </n-space>
        </template>

        <n-divider>通知开关</n-divider>
        <n-space vertical>
          <n-checkbox v-model:checked="form.notifyOnComplete">任务完成通知</n-checkbox>
          <n-checkbox v-model:checked="form.notifyOnError">任务异常通知</n-checkbox>
          <n-checkbox v-model:checked="form.notifyOnStop">手动停止通知</n-checkbox>
          <n-checkbox v-model:checked="form.notifyOnManual">手动运行通知</n-checkbox>
        </n-space>

        <n-space style="margin-top: 20px;">
          <n-button type="primary" :loading="pending === 'save'" :disabled="pending !== null || !loaded" @click="save">保存设置</n-button>
          <n-button :loading="pending === 'test'" :disabled="pending !== null || !loaded" @click="testSend">测试通知</n-button>
        </n-space>
      </n-form>
    </n-card>
  </n-space>
</template>

<script setup>
import { reactive, ref, onMounted, watch } from 'vue'
import { useMessage } from 'naive-ui'
import api from '../api'

const message = useMessage()
const pending = ref('load')
const loaded = ref(false)
const form = reactive({
  notifyType: 'webhook',
  webhookUrl: '',
  telegramToken: '',
  telegramChatId: '',
  clearWebhookUrl: false,
  clearTelegramToken: false,
  notifyOnComplete: true,
  notifyOnError: true,
  notifyOnStop: true,
  notifyOnManual: true,
})
const stored = reactive({ hasWebhookUrl: false, hasTelegramToken: false, telegramChatId: '' })

const load = async () => {
  try {
    // /username 接口同时返回通知配置字段，业务数据在 res.data 内
    const res = await api.get('/username')
    const settings = res.data || {}
    Object.assign(form, {
      notifyType: settings.notifyType || 'webhook',
      webhookUrl: '',
      telegramToken: '',
      telegramChatId: settings.telegramChatId || '',
      clearWebhookUrl: false,
      clearTelegramToken: false,
      notifyOnComplete: settings.notifyOnComplete !== false,
      notifyOnError: settings.notifyOnError !== false,
      notifyOnStop: settings.notifyOnStop !== false,
      notifyOnManual: settings.notifyOnManual !== false,
    })
    stored.hasWebhookUrl = settings.hasWebhookUrl === true
    stored.hasTelegramToken = settings.hasTelegramToken === true
    stored.telegramChatId = settings.telegramChatId || ''
    loaded.value = true
  } catch (e) {
    loaded.value = false
  }
}

watch(() => form.clearWebhookUrl, (clear) => {
  if (clear) form.webhookUrl = ''
})

watch(() => form.clearTelegramToken, (clear) => {
  if (clear) form.telegramToken = ''
})

const notificationsEnabled = () => form.notifyOnComplete || form.notifyOnError || form.notifyOnStop || form.notifyOnManual

const validateChannel = (alwaysRequire = false) => {
  if (!alwaysRequire && !notificationsEnabled()) return true
  if (form.notifyType === 'webhook') {
    const hasWebhook = form.webhookUrl.trim() || (stored.hasWebhookUrl && !form.clearWebhookUrl)
    if (!hasWebhook) {
      message.warning('启用 Webhook 通知时必须填写或保留已保存的 Webhook URL')
      return false
    }
    return true
  }

  const hasToken = form.telegramToken.trim() || (stored.hasTelegramToken && !form.clearTelegramToken)
  if (!hasToken) {
    message.warning('启用 Telegram 通知时必须填写或保留已保存的 Bot Token')
    return false
  }
  if (!form.telegramChatId.trim()) {
    message.warning('启用 Telegram 通知时必须填写 Chat ID')
    return false
  }
  return true
}

const buildPayload = () => ({
  notifyType: form.notifyType,
  webhookUrl: form.webhookUrl.trim(),
  telegramToken: form.telegramToken.trim(),
  telegramChatId: form.telegramChatId.trim(),
  clearWebhookUrl: form.clearWebhookUrl,
  clearTelegramToken: form.clearTelegramToken,
  notifyOnComplete: form.notifyOnComplete,
  notifyOnError: form.notifyOnError,
  notifyOnStop: form.notifyOnStop,
  notifyOnManual: form.notifyOnManual
})

const save = async () => {
  if (pending.value || !loaded.value || !validateChannel()) return
  pending.value = 'save'
  try {
    const payload = buildPayload()
    await api.post('/notifications', payload)
    stored.hasWebhookUrl = payload.clearWebhookUrl ? false : stored.hasWebhookUrl || Boolean(payload.webhookUrl)
    stored.hasTelegramToken = payload.clearTelegramToken ? false : stored.hasTelegramToken || Boolean(payload.telegramToken)
    stored.telegramChatId = payload.telegramChatId
    form.webhookUrl = ''
    form.telegramToken = ''
    form.telegramChatId = payload.telegramChatId
    form.clearWebhookUrl = false
    form.clearTelegramToken = false
    message.success('通知设置已保存')
  } catch (e) {
  } finally {
    pending.value = null
  }
}

const testSend = async () => {
  if (pending.value || !loaded.value || !validateChannel(true)) return
  const payload = form.notifyType === 'webhook'
    ? { webhookUrl: form.webhookUrl.trim(), notifyType: form.notifyType }
    : { telegramToken: form.telegramToken.trim(), telegramChatId: form.telegramChatId.trim(), notifyType: form.notifyType }
  pending.value = 'test'
  try {
    await api.post('/webhook/test', payload)
    message.success('测试通知已发送')
  } catch (e) {
  } finally {
    pending.value = null
  }
}

onMounted(async () => {
  await load()
  pending.value = null
})

const reload = async () => {
  if (pending.value) return
  pending.value = 'load'
  try {
    await load()
  } finally {
    pending.value = null
  }
}
</script>
