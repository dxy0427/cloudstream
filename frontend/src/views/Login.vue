<template>
  <div class="login-container">
    <div class="login-box">
      <n-card class="login-card" :bordered="false" size="huge">
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
              class="custom-input"
            >
              <template #prefix>
                <n-icon color="#808695"><UserOutlined /></n-icon>
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
              class="custom-input"
            >
              <template #prefix>
                <n-icon color="#808695"><LockOutlined /></n-icon>
              </template>
            </n-input>
          </n-form-item>
          
          <div style="margin-top: 20px;">
            <n-button type="primary" block size="large" :loading="loading" @click="handleLogin" color="#18a058">
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
    message.success('登录成功')
    router.push('/dashboard')
  } catch (error) {
    // 拦截器会处理错误提示
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
  /* 深色背景 */
  background-color: #1a1a1a;
  background-image: radial-gradient(#2d2d2d 1px, transparent 1px);
  background-size: 20px 20px;
}

.login-box {
  width: 100%;
  max-width: 420px;
  padding: 20px;
}

.login-card {
  border-radius: 16px;
  box-shadow: 0 8px 30px rgba(0, 0, 0, 0.5);
  background-color: #ffffff; /* 强制卡片为白色背景 */
}

/* 适配暗黑模式下的卡片颜色，如果用户开了暗黑模式 */
:deep(.n-card) {
  transition: background-color 0.3s;
}

.header {
  text-align: center;
  margin-bottom: 30px;
}

.logo {
  font-size: 60px;
  margin-bottom: 10px;
}

h1 {
  margin: 0;
  font-size: 24px;
  font-weight: 700;
  color: #333; /* 强制标题颜色，防止在暗黑模式下变白看不清 */
}

/* 强制输入框样式，确保在白卡片上清晰可见 */
.custom-input {
  background-color: #f7f9fc !important;
  border: 1px solid #e0e0e0;
}
:deep(.n-input__input-el) {
  color: #333 !important;
}

.footer {
  text-align: center;
  margin-top: 20px;
  color: #666;
  font-size: 12px;
}
</style>