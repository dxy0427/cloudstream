import axios from 'axios'
import { createDiscreteApi } from 'naive-ui'

const { message } = createDiscreteApi(['message'])

const api = axios.create({
  baseURL: '/api/v1',
  // 修复：将超时时间延长至 60 秒，防止大目录加载失败
  timeout: 60000
})

api.interceptors.request.use(config => {
  const token = localStorage.getItem('jwt_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

api.interceptors.response.use(
  res => res.data,
  err => {
    if (err.response && err.response.status === 401) {
      if (!window.location.pathname.includes('/login')) {
        localStorage.removeItem('jwt_token')
        window.location.href = '/login'
      }
    }
    // 优化错误提示：处理超时情况
    let msg = '未知错误'
    if (err.code === 'ECONNABORTED' && err.message.includes('timeout')) {
      msg = '请求超时，请检查网络或稍后重试'
    } else {
      msg = err.response?.data?.message || err.response?.data?.error || err.message
    }
    message.error(msg)
    return Promise.reject(err)
  }
)

export default api