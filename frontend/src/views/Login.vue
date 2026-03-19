<template>
  <div class="login-container light-bg">
    <div class="login-box">
      <n-card class="login-card" size="huge" :bordered="false">
        <div class="header">
          <div class="logo">🚀</div>
          <h1>{{ store.siteTitle }}</h1>
        </div>
        <n-form ref="formRef" :model="form" :rules="rules" size="large">
          <n-form-item path="username" label="用户名">
            <n-input
              v-model:value="form.username"
              placeholder="请输入用户名"
              @keydown.enter="handleLogin"
            >
              <template #prefix>
                <n-icon><UserOutlined /></n-icon>
              </template>
            </n-input>
          </n-form-item>
          <n-form-item path="password" label="密码">
            <n-input
              type="password"
              show-password-on="click"
              v-model:value="form.password"
              placeholder="请输入密码"
              @keydown.enter="handleLogin"
            >
              <template #prefix>
                <n-icon><LockOutlined /></n-icon>
              </template>
            </n-input>
          </n-form-item>
          <div style="margin-top: 20px;">
            <n-button type="primary" block size="large" :loading="loading" @click="handleLogin">
              登 录
            </n-button>
          </div>
        </n-form>
      </n-card>
      <div class="footer">CloudStream Media Server</div>
    </div>
  </div>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useMessage, NIcon } from 'naive-ui'
import { UserOutlined, LockOutlined } from '@vicons/antd'
import { useGlobalStore } from '../store/global'
import api from '../api'

const router = useRouter()
const store = useGlobalStore()
const message = useMessage()
const form = reactive({ username: '', password: '' })
const loading = ref(false)

const rules = {
  username: { required: true, message: '请输入用户名', trigger: 'blur' },
  password: { required: true, message: '请输入密码', trigger: 'blur' }
}

const handleLogin = async () => {
  if (!form.username || !form.password) {
    message.warning('请输入完整信息')
    return
  }
  loading.value = true
  try {
    const res = await api.post('/login', form)
    localStorage.setItem('jwt_token', res.token)
    await store.loadSettings({ preserveTheme: true })
    if (res.needsPasswordReminder && !res.passwordReminderShown) {
      localStorage.setItem('needs_password_reminder', '1')
    } else {
      localStorage.removeItem('needs_password_reminder')
    }
    message.success('登录成功')
    router.push('/dashboard')
  } catch (error) {
    const msg = error?.response?.data?.message || error?.response?.data?.error || '用户名或密码错误'
    message.error(msg)
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-container {
  min-height: 100vh;
  width: 100vw;
  display: flex;
  justify-content: center;
  align-items: center;
  transition: background-color 0.3s ease;
  position: relative;
}
.light-bg {
  background-color: #f0f2f5;
  background-image: radial-gradient(#e1e4e8 1px, transparent 1px);
  background-size: 20px 20px;
}
.login-box { width: 100%; max-width: 420px; padding: 20px; }
.login-card { border-radius: 16px; box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1); }
.header { text-align: center; margin-bottom: 30px; }
.logo { font-size: 60px; margin-bottom: 10px; }
h1 { margin: 0; font-size: 24px; font-weight: 700; }
.footer { text-align: center; margin-top: 20px; font-size: 12px; color: #999; }
</style>
