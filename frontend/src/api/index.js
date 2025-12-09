import axios from 'axios'
import { createDiscreteApi } from 'naive-ui'

const { message } = createDiscreteApi(['message'])

const api = axios.create({
  baseURL: '/api/v1',
  // 核心修复：延长到 60 秒 (60000ms)，解决大目录加载超时
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
      // 防止重复跳转
      if (!window.location.pathname.includes('/login')) {
        localStorage.removeItem('jwt_token')
        window.location.href = '/login'
      }
    }
    
    // 优化错误提示
    let msg = '未知错误'
    if (err.code === 'ECONNABORTED' && err.message.includes('timeout')) {
      msg = '目录加载超时，请重试或检查网络'
    } else {
      msg = err.response?.data?.message || err.response?.data?.error || err.message
    }
    
    message.error(msg)
    return Promise.reject(err)
  }
)

export default api