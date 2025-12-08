<template>
 <n-layout has-sider position="absolute">
  <n-layout-sider
   bordered
   collapse-mode="width"
   :collapsed-width="64"
   :width="240"
   :native-scrollbar="false"
   show-trigger
   v-model:collapsed="collapsed"
  >
   <div style="padding: 16px; font-weight: bold; font-size: 1.2em; display:flex; align-items:center; justify-content: center; overflow: hidden; white-space: nowrap;">
    <span>🚀</span>
    <!-- 修复：增加 mobile-hide 类 -->
    <span v-if="!collapsed" class="mobile-hide" style="margin-left: 10px">{{ store.siteTitle }}</span>
   </div>
   <n-menu
    :options="menuOptions"
    :value="activeKey"
    @update:value="handleUpdateValue"
   />
  </n-layout-sider>
  <n-layout>
   <n-layout-header bordered style="padding: 10px 20px; display: flex; justify-content: space-between; align-items: center;">
     <div></div>
     <n-space align="center">
       <n-switch :value="store.isDark" @update:value="store.toggleTheme">
         <template #checked-icon>🌙</template>
         <template #unchecked-icon>☀️</template>
       </n-switch>
       <n-button strong secondary type="error" size="small" @click="logout">退出</n-button>
     </n-space>
   </n-layout-header>
   <n-layout-content content-style="padding: 16px;">
    <router-view />
   </n-layout-content>
  </n-layout>
 </n-layout>
</template>

<script setup>
import { h, ref, computed } from 'vue'
import { NIcon } from 'naive-ui'
import { useRoute, useRouter } from 'vue-router'
import { useGlobalStore } from '../store/global'
import { DashboardOutlined, CloudOutlined, SyncOutlined, BellOutlined, SettingOutlined } from '@vicons/antd'

const store = useGlobalStore()
const router = useRouter()
const route = useRoute()
const collapsed = ref(false)

function renderIcon(icon) { return () => h(NIcon, null, { default: () => h(icon) }) }

const menuOptions = [
 { label: '仪表盘', key: 'dashboard', icon: renderIcon(DashboardOutlined) },
 { label: '云账户', key: 'accounts', icon: renderIcon(CloudOutlined) },
 { label: '任务管理', key: 'tasks', icon: renderIcon(SyncOutlined) },
 { label: '通知管理', key: 'notifications', icon: renderIcon(BellOutlined) },
 { label: '安全设置', key: 'settings', icon: renderIcon(SettingOutlined) },
]

const activeKey = computed(() => route.path.substring(1))
function handleUpdateValue(key) { router.push('/' + key) }
function logout() { localStorage.removeItem('jwt_token'); router.push('/login') }
</script>

<style scoped>
@media (max-width: 600px) {
  .mobile-hide { display: none; }
}
</style>