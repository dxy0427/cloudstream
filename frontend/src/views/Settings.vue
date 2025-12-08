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
    
    <n-divider>修改凭证</n-divider>
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
    <n-button type="primary" block @click="submit">更新凭证</n-button>
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
  confirmPassword: ''
})

onMounted(async () => {
 const res = await api.get('/username')
 username.value = res.data.username
})

const saveTitle = () => {
  store.setSiteTitle(titleForm.title)
  message.success('网站标题已更新')
}

const submit = async () => {
 if(!form.currentPassword) return message.error('请输入当前密码')
 await api.post('/update_credentials', form)
 message.success('凭证已修改，请重新登录')
 setTimeout(() => {
   localStorage.removeItem('jwt_token')
   window.location.reload()
 }, 1000)
}
</script>