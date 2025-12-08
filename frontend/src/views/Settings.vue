<template>
 <n-space vertical>
  <n-card title="全局显示设置" style="max-width: 600px">
    <n-form label-placement="left" label-width="120">
      <n-form-item label="网站标题">
        <n-input v-model:value="titleForm.title" placeholder="CloudStream" />
        <n-button type="primary" style="margin-left: 10px" @click="saveTitle">保存</n-button>
      </n-form-item>
    </n-form>
  </n-card>

  <n-card title="账户设置" style="max-width: 600px">
   <n-form ref="formRef" :model="form">
    <n-form-item label="当前用户名">
      <n-input :value="username" disabled />
    </n-form-item>
    
    <n-divider>Webhook 通知</n-divider>
    <n-form-item label="通知 URL">
      <n-input v-model:value="form.webhookUrl" placeholder="例如 Bark 或 自定义 API 地址" />
    </n-form-item>
    
    <n-divider>安全修改</n-divider>
    <n-form-item label="当前密码" path="currentPassword" required>
      <n-input type="password" show-password-on="click" v-model:value="form.currentPassword" placeholder="修改任何设置都需要验证当前密码" />
    </n-form-item>
    <n-form-item label="新用户名" path="newUsername">
      <n-input v-model:value="form.newUsername" placeholder="留空不修改" />
    </n-form-item>
    <n-form-item label="新密码" path="newPassword">
      <n-input type="password" show-password-on="click" v-model:value="form.newPassword" placeholder="留空不修改" />
    </n-form-item>
    <n-form-item label="确认新密码" path="confirmPassword">
      <n-input type="password" show-password-on="click" v-model:value="form.confirmPassword" />
    </n-form-item>
    <n-button type="primary" block @click="submit">保存设置</n-button>
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
  webhookUrl: '' // 新增
})

onMounted(async () => {
 const res = await api.get('/username')
 username.value = res.data.username
 form.webhookUrl = res.data.webhook // 回显 webhook
})

const saveTitle = () => {
  store.setSiteTitle(titleForm.title)
  message.success('网站标题已更新')
}

const submit = async () => {
 if(!form.currentPassword) return message.error('请输入当前密码以保存设置')
 await api.post('/update_credentials', form)
 // 如果改了密码或用户名，才需要重新登录
 if (form.newPassword || (form.newUsername && form.newUsername !== username.value)) {
    message.success('凭证已修改，请重新登录')
    setTimeout(() => {
      localStorage.removeItem('jwt_token')
      window.location.reload()
    }, 1000)
 } else {
    message.success('设置已保存')
 }
}
</script>