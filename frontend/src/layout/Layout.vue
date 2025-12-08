<template>
  <n-layout has-sider position="absolute">
    <n-layout-sider
      bordered
      collapse-mode="width"
      :collapsed-width="64"
      :width="240"
      :native-scrollbar="false"
      show-trigger
    >
      <div style="padding: 16px; font-weight: bold; font-size: 1.2em; display:flex; align-items:center; gap:10px">
        <span>🚀</span>
        <span v-if="!collapsed">CloudStream</span>
      </div>
      <n-menu
        :options="menuOptions"
        :value="activeKey"
        @update:value="handleUpdateValue"
      />
    </n-layout-sider>
    <n-layout>
      <n-layout-header bordered style="padding: 10px 20px; display: flex; justify-content: flex-end; align-items: center;">
         <n-button strong secondary type="error" size="small" @click="logout">退出登录</n-button>
      </n-layout-header>
      <n-layout-content content-style="padding: 24px;">
        <router-view />
      </n-layout-content>
    </n-layout>
  </n-layout>
</template>

<script setup>
import { h, ref, computed } from 'vue'
import { NIcon } from 'naive-ui'
import { useRoute, useRouter } from 'vue-router'
import {
  DashboardOutlined,
  CloudOutlined,
  SyncOutlined,
  SettingOutlined
} from '@vicons/antd'

function renderIcon(icon) {
  return () => h(NIcon, null, { default: () => h(icon) })
}

const menuOptions = [
  { label: '仪表盘', key: 'dashboard', icon: renderIcon(DashboardOutlined) },
  { label: '云账户', key: 'accounts', icon: renderIcon(CloudOutlined) },
  { label: '任务管理', key: 'tasks', icon: renderIcon(SyncOutlined) },
  { label: '安全设置', key: 'settings', icon: renderIcon(SettingOutlined) },
]

const router = useRouter()
const route = useRoute()

const activeKey = computed(() => route.path.substring(1))

function handleUpdateValue(key) {
  router.push('/' + key)
}

function logout() {
  localStorage.removeItem('jwt_token')
  router.push('/login')
}
</script>