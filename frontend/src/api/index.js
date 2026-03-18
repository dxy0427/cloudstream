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
      return Promise.reject(err)
    }
    
    // 统一错误提示，按状态码分类
    let msg = '未知错误'
    const status = err.response?.status
    const data = err.response?.data
    
    if (err.code === 'ECONNABORTED' && err.message.includes('timeout')) {
      msg = '请求超时，请重试或检查网络'
    } else if (status === 400) {
      msg = data?.message || data?.error || '请求参数错误'
    } else if (status === 401) {
      msg = '未授权，请重新登录'
    } else if (status === 403) {
      msg = '无权限访问'
    } else if (status === 404) {
      msg = '请求的资源不存在'
    } else if (status === 429) {
      msg = '请求过于频繁，请稍后再试'
    } else if (status === 500) {
      msg = '服务器错误，请稍后重试'
    } else if (status === 502 || status === 503 || status === 504) {
      msg = '服务暂时不可用，请稍后重试'
    } else if (data?.message) {
      msg = data.message
    } else if (data?.error) {
      msg = data.error
    } else if (err.message) {
      msg = err.message
    }
    
    message.error(msg)
    return Promise.reject(err)
  }
)

// 用户个性化设置 API
export const userSettingsApi = {
  // 获取用户设置
  getSettings: () => api.get('/user/settings'),
  
  // 更新用户设置
  updateSettings: (data) => api.post('/user/settings', data)
}

export default api