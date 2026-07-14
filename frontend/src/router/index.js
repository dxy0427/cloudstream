import { createRouter, createWebHistory } from 'vue-router'
import Layout from '../layout/Layout.vue'
import api, {
  hasAuthenticatedSession,
  markAuthenticatedSession,
  clearAuthenticatedSession,
  sanitizeInternalRedirect,
  isLoginPath
} from '../api'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      component: () => import('../views/Login.vue'),
      meta: { noAuth: true }
    },
    {
      path: '/',
      component: Layout,
      redirect: '/dashboard',
      children: [
        { path: 'dashboard', component: () => import('../views/Dashboard.vue') },
        { path: 'accounts', component: () => import('../views/Accounts.vue') },
        { path: 'tasks', component: () => import('../views/Tasks.vue') },
        { path: 'notifications', component: () => import('../views/Notifications.vue') },
        { path: 'mediaserver', component: () => import('../views/MediaServer.vue') },
        { path: 'settings', component: () => import('../views/Settings.vue') }
      ]
    },
    { path: '/:pathMatch(.*)*', redirect: '/dashboard' }
  ]
})

// HTTP-only Cookie 不可被 JS 读取；用 /username 探测会话
router.beforeEach(async (to) => {
  if (isLoginPath(to.path)) {
    const redirect = sanitizeInternalRedirect(typeof to.query.redirect === 'string' ? to.query.redirect : '/dashboard')
    if (hasAuthenticatedSession()) return redirect
    try {
      await api.get('/username', { skipAuthRedirect: true, skipErrorToast: true })
      markAuthenticatedSession()
      return redirect
    } catch {
      if (to.path !== '/login') {
        return { path: '/login', query: to.query, hash: to.hash, replace: true }
      }
      return true
    }
  }
  if (to.meta.noAuth) {
    return true
  }
  if (hasAuthenticatedSession()) return true
  try {
    await api.get('/username', { skipAuthRedirect: true, skipErrorToast: true })
    markAuthenticatedSession()
    return true
  } catch {
    clearAuthenticatedSession()
    return { path: '/login', query: { redirect: to.fullPath } }
  }
})

export default router
