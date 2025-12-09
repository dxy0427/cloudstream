import { createRouter, createWebHistory } from 'vue-router'
import Login from '../views/Login.vue'
import Layout from '../layout/Layout.vue' // 确保这里引入的是上面第3步创建的Layout
import Dashboard from '../views/Dashboard.vue'
import Accounts from '../views/Accounts.vue'
import Tasks from '../views/Tasks.vue'
import Settings from '../views/Settings.vue'
import Notifications from '../views/Notifications.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { 
      path: '/login', 
      component: Login, 
      meta: { noAuth: true } 
    },
    {
      path: '/',
      component: Layout,
      redirect: '/dashboard',
      children: [
        { path: 'dashboard', component: Dashboard },
        { path: 'accounts', component: Accounts },
        { path: 'tasks', component: Tasks },
        { path: 'notifications', component: Notifications },
        { path: 'settings', component: Settings }
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