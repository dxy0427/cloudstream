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

// 路由守卫：仅检查是否在登录页，实际认证由 API 拦截器处理
// HTTP-only Cookie 不可被 JS 读取，无法在这里检查
router.beforeEach((to, from, next) => {
  next()
})

export default router
