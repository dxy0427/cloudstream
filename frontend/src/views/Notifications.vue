<template>
  <n-card title="通知管理" style="max-width: 600px">
    <template #header-extra>
      <n-tag type="info">任务开始/完成时推送</n-tag>
    </template>
    
    <n-form ref="formRef" :model="form" label-placement="left" label-width="100">
      <n-form-item label="通知渠道">
        <n-radio-group v-model:value="form.notifyType">
          <n-radio-button value="webhook">Webhook</n-radio-button>
          <n-radio-button value="telegram">Telegram</n-radio-button>
        </n-radio-group>
      </n-form-item>

      <!-- Webhook 配置 -->
      <template v-if="form.notifyType === 'webhook'">
        <n-form-item label="URL 地址">
          <n-input v-model:value="form.webhookUrl" placeholder="http://api.example.com/notify" />
        </n-form-item>
      </template>

      <!-- Telegram 配置 -->
      <template v-else>
        <n-form-item label="Bot Token">
          <n-input type="password" show-password-on="click" v-model:value="form.telegramToken" placeholder="123456:ABC-DEF..." />
        </n-form-item>
        <n-form-item label="Chat ID">
          <n-input v-model:value="form.telegramChatId" placeholder="-100xxxx 或 用户ID" />
        </n-form-item>
      </template>

      <n-divider />

      <n-space justify="end">
        <n-button @click="testNotify">发送测试</n-button>
        <n-button type="primary" @click="saveNotify">保存配置</n-button>
      </n-space>
    </n-form>
  </n-card>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import api from '../api'

const message = useMessage()
const form = reactive({ 
  notifyType: 'webhook',
  webhookUrl: '',
  telegramToken: '',
  telegramChatId: ''
})

onMounted(async () => {
 const res = await api.get('/username')
 form.notifyType = res.data.notifyType || 'webhook'
 form.webhookUrl = res.data.webhookUrl
 form.telegramToken = res.data.telegramToken
 form.telegramChatId = res.data.telegramChatId
})

const testNotify = async () => {
  const payload = { type: form.notifyType }
  if (form.notifyType === 'telegram') {
      if (!form.telegramToken || !form.telegramChatId) return message.warning('请填写 Token 和 Chat ID')
      payload.token = form.telegramToken
      payload.chatId = form.telegramChatId
  } else {
      if (!form.webhookUrl) return message.warning('请填写 Webhook URL')
      // 修复：增加 http 校验
      if (!/^https?:\/\//.test(form.webhookUrl)) {
          return message.error('URL 必须以 http:// 或 https:// 开头')
      }
      payload.url = form.webhookUrl
  }

  try {
    const res = await api.post('/webhook/test', payload)
    message.success(res.message)
  } catch (e) {}
}

const saveNotify = async () => {
  if (form.notifyType === 'webhook' && form.webhookUrl && !/^https?:\/\//.test(form.webhookUrl)) {
      return message.error('Webhook URL 格式不正确')
  }
  try {
    await api.post('/notifications', form)
    message.success('通知配置已保存')
  } catch (e) {}
}
</script>