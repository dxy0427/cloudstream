import { defineStore } from 'pinia'
import { ref } from 'vue'
import { userSettingsApi } from '../api'

export const useGlobalStore = defineStore('global', () => {
  // 从 localStorage 读取缓存（用于快速响应）
  const cachedTitle = localStorage.getItem('site_title')
  const cachedTheme = localStorage.getItem('theme')
  
  const siteTitle = ref(cachedTitle || 'CloudStream')
  const isDark = ref(cachedTheme !== 'light')

  // 从后端加载用户设置
  const loadSettings = async () => {
    try {
      const res = await userSettingsApi.getSettings()
      if (res.code === 0 && res.data) {
        siteTitle.value = res.data.siteTitle || 'CloudStream'
        isDark.value = res.data.theme !== 'light'
        
        // 更新 localStorage 缓存
        localStorage.setItem('site_title', siteTitle.value)
        localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
        document.title = siteTitle.value
      }
    } catch (error) {
      console.error('加载用户设置失败:', error)
    }
  }

  const toggleTheme = async () => {
    const newTheme = !isDark.value ? 'dark' : 'light'
    await updateSettings({ theme: newTheme })
  }

  const setSiteTitle = async (newTitle) => {
    // 修复：如果为空，恢复默认
    const title = newTitle && newTitle.trim() ? newTitle : 'CloudStream'
    await updateSettings({ siteTitle: title })
  }

  // 统一更新设置的函数
  const updateSettings = async (data) => {
    try {
      const res = await userSettingsApi.updateSettings(data)
      if (res.code === 0 && res.data) {
        siteTitle.value = res.data.siteTitle
        isDark.value = res.data.theme !== 'light'
        
        // 更新 localStorage 缓存
        localStorage.setItem('site_title', siteTitle.value)
        localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
        document.title = siteTitle.value
      }
    } catch (error) {
      console.error('更新用户设置失败:', error)
    }
  }

  // 初始化时设置页面标题
  document.title = siteTitle.value

  return { 
    siteTitle, 
    isDark, 
    toggleTheme, 
    setSiteTitle,
    loadSettings
  }
})