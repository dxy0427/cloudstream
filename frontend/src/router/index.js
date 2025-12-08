import { createRouter, createWebHistory } from 'vue-router'
import Login from '../views/Login.vue'
import Layout from '../layout/Layout.vue'
import Dashboard from '../views/Dashboard.vue'
import Accounts from '../views/Accounts.vue'
import Tasks from '../views/Tasks.vue'
import Settings from '../views/Settings.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: Login, meta: { noAuth: true } },
    {
      path: '/',
      component: Layout,
      redirect: '/dashboard',
      children: [
        { path: 'dashboard', component: Dashboard, name: '仪表盘' },
        { path: 'accounts', component: Accounts, name: '云账户' },
        { path: 'tasks', component: Tasks, name: '任务管理' },
        { path: 'settings', component: Settings, name: '设置' }
      ]
    }
  ]
})

router.beforeEach((to, from, next) => {
  const token = localStorage.getItem('jwt_token')
  if (!token && !to.meta.noAuth) {
    next('/login')
  } else {
    next()
  }
})

export default router