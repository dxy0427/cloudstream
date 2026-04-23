import axios from 'axios'
import { createDiscreteApi } from 'naive-ui'

const { message } = createDiscreteApi(['message'])

const api = axios.create({
  baseURL: '/api/v1',
  timeout: 60000,
  withCredentials: true  // 自动携带 HTTP-only Cookie
})

api.interceptors.response.use(
  res => res.data,
  err => {
    const status = err.response?.status
    const data = err.response?.data

    if (status === 401) {
      if (!window.location.pathname.includes('/login')) {
        window.location.href = '/login'
      }
      return Promise.reject(err)
    }

    let msg = '未知错误'

    if (err.code === 'ECONNABORTED' && err.message.includes('timeout')) {
      msg = '请求超时，请重试或检查网络'
    } else if (status === 400) {
      msg = data?.message || data?.error || '请求参数错误'
    } else if (status === 403) {
      msg = '无权限访问'
    } else if (status === 404) {
      msg = '请求的资源不存在'
    } else if (status === 429) {
      msg = data?.message || data?.error || '请求过于频繁，请稍后再试'
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

export const userSettingsApi = {
  getSettings: () => api.get('/user/settings'),
  updateSettings: (data) => api.post('/user/settings', data)
}

export default api
