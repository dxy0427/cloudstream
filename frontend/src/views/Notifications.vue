<template>
  <n-space vertical>
    <n-card title="通知管理">
      <n-form label-placement="top">
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
             <n-input v-model:value="form.webhookUrl" :placeholder="stored.hasWebhookUrl ? '已配置，留空不修改' : '请输入机器人 Webhook 地址'" />
          </n-form-item>
        </template>

        <template v-else>
          <n-form-item label="Telegram Bot Token">
             <n-input type="password" show-password-on="click" v-model:value="form.telegramToken" :placeholder="stored.hasTelegramToken ? '已配置，留空不修改' : '请输入 Bot Token'" />
          </n-form-item>
          <n-form-item label="Telegram Chat ID">
            <n-input v-model:value="form.telegramChatId" placeholder="请输入 Chat ID" />
          </n-form-item>
        </template>

        <n-divider>通知开关</n-divider>
        <n-space vertical>
          <n-checkbox v-model:checked="form.notifyOnComplete">任务完成通知</n-checkbox>
          <n-checkbox v-model:checked="form.notifyOnError">任务异常通知</n-checkbox>
          <n-checkbox v-model:checked="form.notifyOnStop">手动停止通知</n-checkbox>
          <n-checkbox v-model:checked="form.notifyOnManual">手动运行通知</n-checkbox>
        </n-space>

        <n-space style="margin-top: 20px;">
          <n-button type="primary" @click="save">保存设置</n-button>
          <n-button @click="testSend">测试通知</n-button>
        </n-space>
      </n-form>
    </n-card>
  </n-space>
</template>

<script setup>
import { reactive, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import api from '../api'

const message = useMessage()
const form = reactive({
  notifyType: 'webhook',
  webhookUrl: '',
  telegramToken: '',
  telegramChatId: '',
  notifyOnComplete: true,
  notifyOnError: true,
  notifyOnStop: true,
  notifyOnManual: true,
})
const stored = reactive({ hasWebhookUrl: false, hasTelegramToken: false })

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
      notifyOnComplete: settings.notifyOnComplete !== false,
      notifyOnError: settings.notifyOnError !== false,
      notifyOnStop: settings.notifyOnStop !== false,
      notifyOnManual: settings.notifyOnManual !== false,
    })
    stored.hasWebhookUrl = settings.hasWebhookUrl === true
    stored.hasTelegramToken = settings.hasTelegramToken === true
  } catch (e) {}
}

const save = async () => {
  try {
    await api.post('/notifications', form)
    if (form.webhookUrl) stored.hasWebhookUrl = true
    if (form.telegramToken) stored.hasTelegramToken = true
    form.webhookUrl = ''
    form.telegramToken = ''
    message.success('通知设置已保存')
  } catch (e) {}
}

const testSend = async () => {
  const payload = form.notifyType === 'webhook'
    ? { webhookUrl: form.webhookUrl, notifyType: form.notifyType }
    : { telegramToken: form.telegramToken, telegramChatId: form.telegramChatId, notifyType: form.notifyType }
  try {
    await api.post('/webhook/test', payload)
    message.success('测试通知已发送')
  } catch (e) {}
}

onMounted(load)
</script>
