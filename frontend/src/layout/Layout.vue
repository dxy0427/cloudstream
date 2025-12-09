<template>
  <n-layout position="absolute">
    <n-layout-header bordered style="height: 64px; padding: 0 15px; display: flex; align-items: center; justify-content: space-between; z-index: 2000;">
      <div style="display: flex; align-items: center; gap: 15px;">
        <n-button text style="font-size: 24px;" @click="toggleSidebar">
          <n-icon>
            <MenuUnfoldOutlined v-if="collapsed" />
            <MenuFoldOutlined v-else />
          </n-icon>
        </n-button>
        <div style="font-weight: bold; font-size: 1.2rem; display: flex; align-items: center; gap: 8px; cursor: pointer;" @click="$router.push('/')">
          <span style="font-size: 1.4rem;">🚀</span>
          <n-text tag="span" strong>{{ store.siteTitle }}</n-text>
        </div>
      </div>
      <n-space align="center">
        <n-switch :value="store.isDark" @update:value="store.toggleTheme">
          <template #checked-icon>🌙</template>
          <template #unchecked-icon>☀️</template>
        </n-switch>
        <n-button strong secondary type="error" size="small" @click="logout">退出</n-button>
      </n-space>
    </n-layout-header>

    <n-layout has-sider position="absolute" style="top: 64px; bottom: 0;">
      <n-layout-sider
        bordered
        collapse-mode="transform"
        :collapsed-width="0" 
        :width="240"
        :collapsed="collapsed"
        :native-scrollbar="false"
        style="z-index: 1000; height: 100%;"
        @update:collapsed="(val) => collapsed = val"
      >
        <n-menu :options="menuOptions" :value="activeKey" @update:value="handleMenuClick" />
      </n-layout-sider>
      <n-layout-content content-style="padding: 16px; min-height: 100%; transition: all 0.3s;" :native-scrollbar="false">
        <div v-if="!collapsed && isMobile" class="mobile-mask" @click="collapsed = true"></div>
        <router-view />
      </n-layout-content>
    </n-layout>
  </n-layout>
</template>

<script setup>
import { h, ref, computed, onMounted } from 'vue'
import { NIcon, NText } from 'naive-ui'
import { useRoute, useRouter } from 'vue-router'
import { useGlobalStore } from '../store/global'
import api from '../api'
import { DashboardOutlined, CloudOutlined, SyncOutlined, BellOutlined, SettingOutlined, MenuFoldOutlined, MenuUnfoldOutlined } from '@vicons/antd'

const store = useGlobalStore()
const router = useRouter()
const route = useRoute()
const collapsed = ref(true)
const isMobile = ref(false)

const checkMobile = () => {
  isMobile.value = window.innerWidth <= 768
  if (isMobile.value) collapsed.value = true
  else collapsed.value = false 
}

onMounted(() => {
  checkMobile()
  window.addEventListener('resize', checkMobile)
})

function renderIcon(icon) { return () => h(NIcon, null, { default: () => h(icon) }) }

const menuOptions = [
  { label: '仪表盘', key: 'dashboard', icon: renderIcon(DashboardOutlined) },
  { label: '云账户', key: 'accounts', icon: renderIcon(CloudOutlined) },
  { label: '任务管理', key: 'tasks', icon: renderIcon(SyncOutlined) },
  { label: '通知管理', key: 'notifications', icon: renderIcon(BellOutlined) },
  { label: '设置管理', key: 'settings', icon: renderIcon(SettingOutlined) },
]

const activeKey = computed(() => {
  const path = route.path.split('/')[1]
  return path || 'dashboard'
})

function toggleSidebar() { collapsed.value = !collapsed.value }

function handleMenuClick(key) {
  router.push('/' + key)
  if (isMobile.value) collapsed.value = true
}

async function logout() {
  try { await api.post('/logout') } catch(e) {}
  localStorage.removeItem('jwt_token')
  router.push('/login')
}
</script>

<style scoped>
.mobile-mask { position: absolute; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0, 0, 0, 0.5); z-index: 900; backdrop-filter: blur(2px); }
</style>