<template>
 <n-space vertical>
  <n-card title="全局显示设置" style="max-width: 600px">
    <n-form label-placement="left" label-width="120" :disabled="titlePending">
      <n-form-item label="网站标题">
        <n-input v-model:value="titleForm.title" :disabled="titlePending" placeholder="CloudStream" />
        <n-button type="primary" style="margin-left: 10px" :loading="titlePending" :disabled="titlePending" @click="saveTitle">保存</n-button>
      </n-form-item>
    </n-form>
  </n-card>

  <n-card title="安全设置" style="max-width: 600px">
   <n-form ref="formRef" :model="form" :disabled="credentialsPending">
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
    <n-button type="primary" block :loading="credentialsPending" :disabled="credentialsPending" @click="submit">确认修改</n-button>
   </n-form>
  </n-card>
 </n-space>
</template>

<script setup>
import { ref, reactive, onMounted, watch } from 'vue'
import { useMessage } from 'naive-ui'
import { useGlobalStore } from '../store/global'
import { hashPassword } from '../utils/crypto'
import api, { clearAuthenticatedSession } from '../api'

const message = useMessage()
const store = useGlobalStore()
const username = ref('')
const titlePending = ref(false)
const credentialsPending = ref(false)

const titleForm = reactive({ title: store.siteTitle })
const form = reactive({ 
  newUsername: '', 
  currentPassword: '', 
  newPassword: '', 
  confirmPassword: ''
})

watch(() => store.siteTitle, (title) => {
  titleForm.title = title
}, { immediate: true })

onMounted(async () => {
 try {
  const res = await api.get('/username')
  username.value = res.data?.username || ''
 } catch (e) {}
})

const saveTitle = async () => {
  if (titlePending.value) return
  const normalizedTitle = titleForm.title.trim()
  if ([...normalizedTitle].length > 64) {
    message.warning('网站标题不能超过 64 个字符')
    return
  }
  titlePending.value = true
  try {
    await store.setSiteTitle(normalizedTitle)
    message.success('网站标题已更新')
  } catch (e) {
  } finally {
    titlePending.value = false
  }
}

const submit = async () => {
 if (credentialsPending.value) return
 if (!form.currentPassword) return message.error('请输入当前密码')
 if (form.newPassword && form.newPassword !== form.confirmPassword) {
   return message.error('两次输入的新密码不一致')
 }
 credentialsPending.value = true
 let clearAll = false
 const currentPassword = form.currentPassword
 const newUsername = form.newUsername
 const newPassword = form.newPassword
 const confirmPassword = form.confirmPassword
 try {
   // 密码 SHA-256 预哈希：明文永远不离开浏览器
   const hashedCurrent = await hashPassword(currentPassword)
   const payload = {
     newUsername,
     currentPassword: hashedCurrent,
     newPassword: newPassword ? await hashPassword(newPassword) : '',
     confirmPassword: confirmPassword ? await hashPassword(confirmPassword) : '',
   }
   const res = await api.post('/update_credentials', payload)
   if (res?.message && res.message.includes('未做任何修改')) {
     clearAll = true
     message.info(res.message)
     return
   }
   clearAll = true
   message.success(res?.message || '凭证已修改，请重新登录')
   // 凭证变更会 bump TokenVersion；显式登出并跳转登录页
   try { await api.post('/logout', null, { skipAuthRedirect: true, skipErrorToast: true }) } catch (e) {}
   clearAuthenticatedSession()
   localStorage.removeItem('needs_password_reminder')
   setTimeout(() => {
     window.location.href = '/login'
   }, 800)
  } catch (e) {
    // 错误已由 api 拦截器全局弹出，此处不重复提示
  } finally {
    if (clearAll) {
      form.newUsername = ''
      form.newPassword = ''
      form.confirmPassword = ''
    }
    form.currentPassword = ''
    credentialsPending.value = false
  }
}
</script>
