<template>
  <n-card title="通知管理" style="max-width: 600px">
    <template #header-extra>
      <n-tag type="info">任务消息推送</n-tag>
    </template>
    
    <!-- 修复：使用 Tabs 替代 Radio Group，手机显示更友好 -->
    <n-tabs type="segment" v-model:value="form.notifyType" animated>
      <n-tab-pane name="webhook" tab="Webhook">
        <n-form label-placement="top" label-width="auto">
          <n-form-item label="URL 地址">
            <n-input v-model:value="form.webhookUrl" placeholder="http://api.example.com/notify" />
          </n-form-item>
        </n-form>
      </n-tab-pane>
      
      <n-tab-pane name="telegram" tab="Telegram">
        <n-form label-placement="top" label-width="auto">
          <n-form-item label="Bot Token">
            <n-input type="password" show-password-on="click" v-model:value="form.telegramToken" placeholder="123456:ABC-DEF..." />
          </n-form-item>
          <n-form-item label="Chat ID">
            <n-input v-model:value="form.telegramChatId" placeholder="-100xxxx 或 用户ID" />
          </n-form-item>
        </n-form>
      </n-tab-pane>
    </n-tabs>

    <n-divider />

    <n-space justify="end">
      <n-button @click="testNotify">发送测试</n-button>
      <n-button type="primary" @click="saveNotify">保存配置</n-button>
    </n-space>
  </n-card>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import api from '../api'

const message = useMessage()
const form = reactive({ notifyType: 'webhook', webhookUrl: '', telegramToken: '', telegramChatId: '' })

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
      if (!/^https?:\/\//.test(form.webhookUrl)) return message.error('URL 必须以 http 开头')
      payload.url = form.webhookUrl
  }
  try { const res = await api.post('/webhook/test', payload); message.success(res.message) } catch (e) {}
}

const saveNotify = async () => {
  try { await api.post('/notifications', form); message.success('已保存') } catch (e) {}
}
</script>