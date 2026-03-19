import { defineStore } from 'pinia'
import { ref } from 'vue'
import { userSettingsApi } from '../api'

export const useGlobalStore = defineStore('global', () => {
  const cachedTitle = localStorage.getItem('site_title')
  const cachedTheme = localStorage.getItem('theme')

  const siteTitle = ref(cachedTitle || 'CloudStream')
  const isDark = ref(cachedTheme !== 'light')

  const applyTitle = (title) => {
    siteTitle.value = title && title.trim() ? title : 'CloudStream'
    localStorage.setItem('site_title', siteTitle.value)
    document.title = siteTitle.value
  }

  const applyTheme = (theme) => {
    isDark.value = theme !== 'light'
    localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
  }

  const applySettings = ({ siteTitle: newTitle, theme }) => {
    if (typeof newTitle === 'string') {
      applyTitle(newTitle)
    }
    if (typeof theme === 'string') {
      applyTheme(theme)
    }
  }

  const loadSettings = async (options = {}) => {
    if (!localStorage.getItem('jwt_token')) {
      return
    }

    try {
      const res = await userSettingsApi.getSettings()
      if (res.code === 0 && res.data) {
        applyTitle(res.data.siteTitle || 'CloudStream')

        if (res.data.theme === 'dark' || res.data.theme === 'light') {
          applyTheme(res.data.theme)
        } else if (options.preserveTheme) {
          applyTheme(isDark.value ? 'dark' : 'light')
        } else {
          applyTheme('dark')
        }
      }
    } catch (error) {
      console.error('加载用户设置失败:', error)
    }
  }

  const updateSettings = async (data) => {
    const hasToken = !!localStorage.getItem('jwt_token')

    if (typeof data.siteTitle === 'string') {
      applyTitle(data.siteTitle)
    }
    if (typeof data.theme === 'string') {
      applyTheme(data.theme)
    }

    if (!hasToken) {
      return { code: 0, data: { siteTitle: siteTitle.value, theme: isDark.value ? 'dark' : 'light' } }
    }

    try {
      const payload = {
        siteTitle: data.siteTitle ?? siteTitle.value,
        theme: data.theme ?? (isDark.value ? 'dark' : 'light')
      }
      const res = await userSettingsApi.updateSettings(payload)
      if (res.code === 0 && res.data) {
        applySettings({
          siteTitle: res.data.siteTitle,
          theme: res.data.theme
        })
      }
      return res
    } catch (error) {
      console.error('更新用户设置失败:', error)
      throw error
    }
  }

  const toggleTheme = async (options = {}) => {
    const newTheme = isDark.value ? 'light' : 'dark'
    return updateSettings({
      theme: newTheme,
      siteTitle: options.syncTitle === false ? undefined : siteTitle.value
    })
  }

  const setSiteTitle = async (newTitle) => {
    const title = newTitle && newTitle.trim() ? newTitle : 'CloudStream'
    return updateSettings({
      siteTitle: title,
      theme: isDark.value ? 'dark' : 'light'
    })
  }

  document.title = siteTitle.value

  return {
    siteTitle,
    isDark,
    toggleTheme,
    setSiteTitle,
    loadSettings,
    updateSettings,
    applyTheme,
    applyTitle
  }
})
