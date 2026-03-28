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
            <n-input v-model:value="form.webhookUrl" placeholder="请输入机器人 Webhook 地址" />
          </n-form-item>
        </template>

        <template v-else>
          <n-form-item label="Telegram Bot Token">
            <n-input v-model:value="form.telegramToken" placeholder="请输入 Bot Token" />
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

const load = async () => {
  try {
    // /username 接口同时返回通知配置字段，且 api 拦截器已 unwrap res.data
    const res = await api.get('/username')
    Object.assign(form, {
      notifyType: res.notifyType || 'webhook',
      webhookUrl: res.webhookUrl || '',
      telegramToken: res.telegramToken || '',
      telegramChatId: res.telegramChatId || '',
      notifyOnComplete: res.notifyOnComplete !== false,
      notifyOnError: res.notifyOnError !== false,
      notifyOnStop: res.notifyOnStop !== false,
      notifyOnManual: res.notifyOnManual !== false,
    })
  } catch (e) {}
}

const save = async () => {
  await api.post('/notifications', form)
  message.success('通知设置已保存')
}

const testSend = async () => {
  const payload = form.notifyType === 'webhook'
    ? { webhookUrl: form.webhookUrl, notifyType: form.notifyType }
    : { telegramToken: form.telegramToken, telegramChatId: form.telegramChatId, notifyType: form.notifyType }
  await api.post('/webhook/test', payload)
  message.success('测试通知已发送')
}

onMounted(load)
</script>
