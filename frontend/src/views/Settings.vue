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

  <n-card title="安全设置" style="max-width: 600px">
   <n-form ref="formRef" :model="form">
    <n-form-item label="当前用户名">
      <n-input :value="username" disabled />
    </n-form-item>
    
    <n-divider>修改账号密码</n-divider>
    <n-form-item label="当前密码" path="currentPassword" required>
      <n-input type="password" show-password-on="click" v-model:value="form.currentPassword" />
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
    <n-button type="primary" block @click="submit">确认修改</n-button>
   </n-form>
  </n-card>
 </n-space>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useGlobalStore } from '../store/global'
import { hashPassword } from '../utils/crypto'
import api from '../api'

const message = useMessage()
const store = useGlobalStore()
const username = ref('')

const titleForm = reactive({ title: store.siteTitle })
const form = reactive({ 
  newUsername: '', 
  currentPassword: '', 
  newPassword: '', 
  confirmPassword: ''
})

onMounted(async () => {
 try {
  const res = await api.get('/username')
  username.value = res.data?.username || ''
 } catch (e) {}
})

const saveTitle = async () => {
  try {
    await store.setSiteTitle(titleForm.title)
    message.success('网站标题已更新')
  } catch (e) {}
}

const submit = async () => {
 if (!form.currentPassword) return message.error('请输入当前密码')
 if (form.newPassword && form.newPassword !== form.confirmPassword) {
   return message.error('两次输入的新密码不一致')
 }
 try {
   // 密码 SHA-256 预哈希：明文永远不离开浏览器
   const hashedCurrent = await hashPassword(form.currentPassword)
   const payload = {
     newUsername: form.newUsername,
     currentPassword: hashedCurrent,
     newPassword: form.newPassword ? await hashPassword(form.newPassword) : '',
     confirmPassword: form.confirmPassword ? await hashPassword(form.confirmPassword) : '',
   }
   await api.post('/update_credentials', payload)
   message.success('凭证已修改，请重新登录')
   setTimeout(() => {
     window.location.reload()
   }, 1000)
 } catch (e) {
   // 错误已由 api 拦截器全局弹出，此处不重复提示
 }
}
</script>
