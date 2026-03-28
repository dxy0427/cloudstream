import { createRouter, createWebHistory } from 'vue-router'
import Layout from '../layout/Layout.vue'

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
    }
  ]
})

function decodeJwtPayload(token) {
  const parts = token.split('.')
  if (parts.length < 2) {
    throw new Error('Invalid JWT format')
  }

  const base64 = parts[1]
    .replace(/-/g, '+')
    .replace(/_/g, '/')
  const paddedBase64 = base64.padEnd(Math.ceil(base64.length / 4) * 4, '=')

  return JSON.parse(atob(paddedBase64))
}

function isTokenExpired(token) {
  try {
    const payload = decodeJwtPayload(token)
    const exp = payload.exp
    if (typeof exp !== 'number') {
      return true
    }
    const now = Math.floor(Date.now() / 1000)
    return exp < now
  } catch (e) {
    return true
  }
}

router.beforeEach((to, from, next) => {
  const token = localStorage.getItem('jwt_token')
  if (!token && !to.meta.noAuth) {
    next('/login')
  } else if (token && !to.meta.noAuth) {
    if (isTokenExpired(token)) {
      localStorage.removeItem('jwt_token')
      next('/login')
    } else {
      next()
    }
  } else {
    next()
  }
})

export default router
