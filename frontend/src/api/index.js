import axios from 'axios'
import { createDiscreteApi } from 'naive-ui'

const { message } = createDiscreteApi(['message'])

let authenticated = false
export const hasAuthenticatedSession = () => authenticated
export const markAuthenticatedSession = () => { authenticated = true }
export const clearAuthenticatedSession = () => { authenticated = false }

export function isLoginPath(path) {
  if (typeof path !== 'string') return false
  const pathname = path.split(/[?#]/, 1)[0].replace(/\/+$/, '') || '/'
  return pathname.toLowerCase() === '/login'
}

/** 仅允许站内相对路径，防止 open redirect */
export function sanitizeInternalRedirect(path) {
  if (typeof path !== 'string' || !path) return '/dashboard'
  if (!path.startsWith('/') || path.startsWith('//')) return '/dashboard'
  if (path.includes('\\') || path.includes('://')) return '/dashboard'
  if (isLoginPath(path)) return '/dashboard'
  return path
}

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
    const skipAuthRedirect = err.config?.skipAuthRedirect === true
    const skipErrorToast = err.config?.skipErrorToast === true

    if (status === 401 && !skipAuthRedirect) {
      clearAuthenticatedSession()
      if (!isLoginPath(window.location.pathname)) {
        const redirect = sanitizeInternalRedirect(window.location.pathname + window.location.search)
        window.location.href = `/login?redirect=${encodeURIComponent(redirect)}`
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
      msg = data?.message || data?.error || '服务器错误，请稍后重试'
    } else if (status === 502 || status === 503 || status === 504) {
      msg = data?.message || data?.error || '服务暂时不可用，请稍后重试'
    } else if (data?.message) {
      msg = data.message
    } else if (data?.error) {
      msg = data.error
    } else if (err.message) {
      msg = err.message
    }

    if (!skipErrorToast) message.error(msg)
    return Promise.reject(err)
  }
)

export const userSettingsApi = {
  getSettings: (config = {}) => api.get('/user/settings', config),
  updateSettings: (data) => api.post('/user/settings', data)
}

export default api
