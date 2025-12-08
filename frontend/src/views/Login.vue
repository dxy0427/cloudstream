<template>
  <div class="login-container">
    <n-card style="width: 400px; padding: 20px;" title="CloudStream 登录" hoverable>
      <n-form ref="formRef" :model="model" :rules="rules">
        <n-form-item path="username" label="用户名">
          <n-input v-model:value="model.username" placeholder="admin" @keydown.enter="login"/>
        </n-form-item>
        <n-form-item path="password" label="密码">
          <n-input type="password" show-password-on="click" v-model:value="model.password" placeholder="admin" @keydown.enter="login"/>
        </n-form-item>
        <n-button type="primary" block @click="login" :loading="loading">
          登录
        </n-button>
      </n-form>
    </n-card>
  </div>
</template>

<script setup>
import { ref, reactive } from 'vue'
import { useMessage } from 'naive-ui'
import { useRouter } from 'vue-router'
import api from '../api'

const message = useMessage()
const router = useRouter()
const loading = ref(false)
const model = reactive({ username: '', password: '' })

const rules = {
  username: { required: true, message: '请输入用户名', trigger: 'blur' },
  password: { required: true, message: '请输入密码', trigger: 'blur' }
}

const login = async () => {
  if(!model.username || !model.password) return
  loading.value = true
  try {
    const res = await api.post('/login', model)
    localStorage.setItem('jwt_token', res.token)
    message.success('登录成功')
    router.push('/')
  } catch (e) {
    // Error handled by interceptor
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
  background: #101014;
}
</style>