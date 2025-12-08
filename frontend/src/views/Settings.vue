<template>
 <n-space vertical>
  <n-card title="全局显示设置" style="max-width: 600px">
    <n-form label-placement="left" label-width="120">
      <n-form-item label="网站标题">
        <n-input v-model:value="titleForm.title" placeholder="CloudStream" />
        <n-button type="primary" style="margin-left: 10px" @click="saveTitle">更新标题</n-button>
      </n-form-item>
    </n-form>
  </n-card>

  <n-card title="系统设置" style="max-width: 600px">
   <n-form ref="formRef" :model="form">
    <n-form-item label="当前用户名">
      <n-input :value="username" disabled />
    </n-form-item>
    
    <n-divider>通知服务</n-divider>
    <n-form-item label="通知渠道">
        <n-radio-group v-model:value="form.notifyType">
            <n-radio-button value="webhook">Webhook (Bark/其他)</n-radio-button>
            <n-radio-button value="telegram">Telegram</n-radio-button>
        </n-radio-group>
    </n-form-item>

    <!-- Webhook 配置 -->
    <template v-if="form.notifyType === 'webhook'">
        <n-form-item label="Webhook URL">
            <n-input v-model:value="form.webhookUrl" placeholder="http://api.example.com/notify" />
        </n-form-item>
    </template>

    <!-- Telegram 配置 -->
    <template v-else>
        <n-form-item label="Bot Token">
            <n-input type="password" show-password-on="click" v-model:value="form.telegramToken" placeholder="123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11" />
        </n-form-item>
        <n-form-item label="Chat ID">
            <n-input v-model:value="form.telegramChatId" placeholder="-100xxxx 或 用户ID" />
        </n-form-item>
    </template>

    <n-form-item>
        <n-button block dashed @click="testNotify">发送测试通知</n-button>
    </n-form-item>
    
    <n-divider>账户安全 (修改需验证密码)</n-divider>
    <n-form-item label="当前密码" path="currentPassword" required>
      <n-input type="password" show-password-on="click" v-model:value="form.currentPassword" placeholder="验证密码以保存以上所有设置" />
    </n-form-item>
    <n-form-item label="新用户名" path="newUsername">
      <n-input v-model:value="form.newUsername" placeholder="不修改请留空" />
    </n-form-item>
    <n-form-item label="新密码" path="newPassword">
      <n-input type="password" show-password-on="click" v-model:value="form.newPassword" placeholder="不修改请留空" />
    </n-form-item>
    <n-form-item label="确认新密码" path="confirmPassword">
      <n-input type="password" show-password-on="click" v-model:value="form.confirmPassword" />
    </n-form-item>
    
    <n-button type="primary" block size="large" @click="submit">保存所有设置</n-button>
   </n-form>
  </n-card>
 </n-space>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useGlobalStore } from '../store/global'
import api from '../api'

const message = useMessage()
const store = useGlobalStore()
const username = ref('')

const titleForm = reactive({ title: store.siteTitle })
const form = reactive({ 
  newUsername: '', 
  currentPassword: '', 
  newPassword: '', 
  confirmPassword: '',
  
  notifyType: 'webhook',
  webhookUrl: '',
  telegramToken: '',
  telegramChatId: ''
})

onMounted(async () => {
 const res = await api.get('/username')
 username.value = res.data.username
 form.notifyType = res.data.notifyType || 'webhook'
 form.webhookUrl = res.data.webhookUrl
 form.telegramToken = res.data.telegramToken
 form.telegramChatId = res.data.telegramChatId
})

const saveTitle = () => {
  store.setSiteTitle(titleForm.title)
  message.success('网站标题已更新')
}

const testNotify = async () => {
  const payload = { type: form.notifyType }
  if (form.notifyType === 'telegram') {
      if (!form.telegramToken || !form.telegramChatId) return message.warning('请填写 Token 和 Chat ID')
      payload.token = form.telegramToken
      payload.chatId = form.telegramChatId
  } else {
      if (!form.webhookUrl) return message.warning('请填写 Webhook URL')
      payload.url = form.webhookUrl
  }

  try {
    const res = await api.post('/webhook/test', payload)
    message.success(res.message)
  } catch (e) {}
}

const submit = async () => {
 if(!form.currentPassword) return message.error('请输入当前密码以保存设置')
 await api.post('/update_credentials', form)
 if (form.newPassword || (form.newUsername && form.newUsername !== username.value)) {
    message.success('凭证已修改，请重新登录')
    setTimeout(() => {
      localStorage.removeItem('jwt_token')
      window.location.reload()
    }, 1000)
 } else {
    message.success('所有设置已保存')
 }
}
</script>