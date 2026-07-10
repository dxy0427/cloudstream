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
        <router-view v-slot="{ Component }">
          <keep-alive include="Dashboard">
            <component :is="Component" />
          </keep-alive>
        </router-view>
      </n-layout-content>
    </n-layout>
  </n-layout>
</template>

<script setup>
import { h, ref, computed, onMounted, onUnmounted } from 'vue'
import { NIcon, NText, useDialog } from 'naive-ui'
import { useRoute, useRouter } from 'vue-router'
import { useGlobalStore } from '../store/global'
import api, { clearAuthenticatedSession } from '../api'
import {
  DashboardOutlined,
  CloudOutlined,
  SyncOutlined,
  BellOutlined,
  SettingOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  PlayCircleOutlined
} from '@vicons/antd'

const store = useGlobalStore()
const router = useRouter()
const route = useRoute()
const dialog = useDialog()
const collapsed = ref(true)
const isMobile = ref(false)

const checkMobile = () => {
  isMobile.value = window.innerWidth <= 768
  collapsed.value = isMobile.value
}

const showPasswordReminderIfNeeded = async () => {
  if (localStorage.getItem('needs_password_reminder') !== '1') {
    return
  }

  dialog.warning({
    title: '安全提醒',
    content: '当前账号仍在使用默认管理员密码，建议尽快到“设置管理”里修改密码。此提醒不会强制你立刻修改。',
    positiveText: '去设置',
    negativeText: '稍后再说',
    onPositiveClick: async () => {
      localStorage.removeItem('needs_password_reminder')
      try {
        await api.post('/user/password-reminder/dismiss')
      } catch (e) {}
      router.push('/settings')
    },
    onNegativeClick: async () => {
      localStorage.removeItem('needs_password_reminder')
      try {
        await api.post('/user/password-reminder/dismiss')
      } catch (e) {}
    }
  })
}

onMounted(() => {
  checkMobile()
  window.addEventListener('resize', checkMobile)
	store.loadSettings()
  showPasswordReminderIfNeeded()
})

onUnmounted(() => {
  window.removeEventListener('resize', checkMobile)
})

function renderIcon(icon) { return () => h(NIcon, null, { default: () => h(icon) }) }

const menuOptions = [
  { label: '仪表盘', key: 'dashboard', icon: renderIcon(DashboardOutlined) },
  { label: '云账户', key: 'accounts', icon: renderIcon(CloudOutlined) },
  { label: '任务管理', key: 'tasks', icon: renderIcon(SyncOutlined) },
  { label: '媒体服务器', key: 'mediaserver', icon: renderIcon(PlayCircleOutlined) },
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
  try { await api.post('/logout') } catch (e) {}
  localStorage.removeItem('needs_password_reminder')
	clearAuthenticatedSession()
  router.push('/login')
}
</script>

<style scoped>
.mobile-mask { position: absolute; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0, 0, 0, 0.5); z-index: 900; backdrop-filter: blur(2px); }
</style>
