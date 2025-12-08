import { createRouter, createWebHistory } from 'vue-router'
import { createDiscreteApi } from 'naive-ui'
import Login from '../views/Login.vue'
import Layout from '../layout/Layout.vue'
import Dashboard from '../views/Dashboard.vue'
import Accounts from '../views/Accounts.vue'
import Tasks from '../views/Tasks.vue'
import Settings from '../views/Settings.vue'
import Notifications from '../views/Notifications.vue'

const router = createRouter({
 history: createWebHistory(),
 routes: [
  { path: '/login', component: Login, meta: { noAuth: true } },
  {
   path: '/',
   component: Layout,
   redirect: '/dashboard', // 这里确保了默认直接进入仪表盘
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

// 进度条控制
let loadingBar = null

router.beforeEach((to, from, next) => {
 if (!loadingBar) {
     const { loadingBar: lb } = createDiscreteApi(['loadingBar'])
     loadingBar = lb
 }
 loadingBar.start()

 const token = localStorage.getItem('jwt_token')
 if (!token && !to.meta.noAuth) {
  next('/login')
 } else {
  next()
 }
})

router.afterEach(() => {
 if (loadingBar) loadingBar.finish()
})

export default router