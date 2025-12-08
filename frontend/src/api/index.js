import axios from 'axios'
import { createDiscreteApi } from 'naive-ui'

const { message } = createDiscreteApi(['message'])

const api = axios.create({
  baseURL: '/api/v1',
  timeout: 10000
})

api.interceptors.request.use(config => {
  const token = localStorage.getItem('jwt_token')
  if (token) {
    // 修复：使用模板字符串正确拼接 Token
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

api.interceptors.response.use(
  res => res.data,
  err => {
    if (err.response && err.response.status === 401) {
      localStorage.removeItem('jwt_token')
      window.location.href = '/login'
    }
    const msg = err.response?.data?.message || err.response?.data?.error || err.message
    message.error(msg)
    return Promise.reject(err)
  }
)

export default api