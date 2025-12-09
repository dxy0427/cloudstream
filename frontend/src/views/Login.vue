<template>
  <div class="login-container">
    <n-card class="login-card" hoverable>
      <div class="login-header">
        <div class="logo">🚀</div>
        <h2>{{ store.siteTitle }}</h2>
      </div>
      <n-form ref="formRef" :model="form" :rules="rules">
        <n-form-item path="username" label="用户名">
          <n-input v-model:value="form.username" placeholder="请输入用户名" @keydown.enter="handleLogin" autofocus />
        </n-form-item>
        <n-form-item path="password" label="密码">
          <n-input
            type="password"
            show-password-on="click"
            v-model:value="form.password"
            placeholder="请输入密码"
            @keydown.enter="handleLogin"
          />
        </n-form-item>
        <n-button type="primary" block :loading="loading" @click="handleLogin" size="large">
          登 录
        </n-button>
      </n-form>
    </n-card>
  </div>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useMessage } from 'naive-ui'
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
    message.warning('请输入用户名和密码')
    return
  }
  
  loading.value = true
  try {
    const res = await api.post('/login', form)
    // 保存 Token
    localStorage.setItem('jwt_token', res.token)
    message.success('登录成功')
    // 跳转到仪表盘
    router.push('/dashboard')
  } catch (error) {
    // 错误已由拦截器处理，这里只需重置加载状态
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-container {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 100vh;
  background-color: #2c3e50;
  background-image: linear-gradient(135deg, #2c3e50 0%, #000000 100%);
}

.login-card {
  width: 100%;
  max-width: 400px;
  border-radius: 12px;
  box-shadow: 0 8px 20px rgba(0, 0, 0, 0.3);
}

.login-header {
  text-align: center;
  margin-bottom: 24px;
}

.logo {
  font-size: 48px;
  margin-bottom: 10px;
  animation: float 3s ease-in-out infinite;
}

h2 {
  margin: 0;
  font-weight: 600;
  color: #333;
}

/* 适配暗色模式 */
:deep(.n-card) {
  background-color: rgba(255, 255, 255, 0.95);
}

@keyframes float {
  0% { transform: translateY(0px); }
  50% { transform: translateY(-10px); }
  100% { transform: translateY(0px); }
}
</style>