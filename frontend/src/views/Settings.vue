<template>
  <n-card title="账户安全设置" style="max-width: 500px">
    <n-form ref="formRef" :model="form">
      <n-form-item label="当前用户名">
         <n-input :value="username" disabled />
      </n-form-item>
      <n-form-item label="新用户名" path="newUsername">
         <n-input v-model:value="form.newUsername" placeholder="留空不修改" />
      </n-form-item>
      <n-form-item label="当前密码" path="currentPassword" required>
         <n-input type="password" show-password-on="click" v-model:value="form.currentPassword" />
      </n-form-item>
      <n-form-item label="新密码" path="newPassword">
         <n-input type="password" show-password-on="click" v-model:value="form.newPassword" placeholder="留空不修改" />
      </n-form-item>
      <n-form-item label="确认新密码" path="confirmPassword">
         <n-input type="password" show-password-on="click" v-model:value="form.confirmPassword" />
      </n-form-item>
      <n-button type="primary" block @click="submit">更新凭证</n-button>
    </n-form>
  </n-card>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import api from '../api'

const message = useMessage()
const username = ref('')
const form = reactive({ newUsername: '', currentPassword: '', newPassword: '', confirmPassword: '' })

onMounted(async () => {
  const res = await api.get('/username')
  username.value = res.data.username
})

const submit = async () => {
  if(!form.currentPassword) return message.error('请输入当前密码')
  await api.post('/update_credentials', form)
  message.success('修改成功，请重新登录')
  setTimeout(() => {
     localStorage.removeItem('jwt_token')
     window.location.reload()
  }, 1000)
}
</script>