import { defineStore } from 'pinia'
import { ref } from 'vue'
import { userSettingsApi } from '../api'

export const useGlobalStore = defineStore('global', () => {
  const cachedTitle = localStorage.getItem('site_title')
  const cachedTheme = localStorage.getItem('theme')

  const siteTitle = ref(cachedTitle || 'CloudStream')
  const isDark = ref(cachedTheme === 'dark')
  const settingsLoaded = ref(false)

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

  const getCurrentSettings = () => ({
    siteTitle: siteTitle.value,
    theme: isDark.value ? 'dark' : 'light'
  })

  const normalizeSettings = (settings, fallback = getCurrentSettings()) => ({
    siteTitle: typeof settings?.siteTitle === 'string' && settings.siteTitle.trim()
      ? settings.siteTitle.trim()
      : fallback.siteTitle,
    theme: settings?.theme === 'dark' || settings?.theme === 'light'
      ? settings.theme
      : fallback.theme
  })

  let confirmedSettings = getCurrentSettings()
  let settingsQueue = Promise.resolve()
  let settingsRequestSeq = 0

  const enqueueSettingsRequest = (operation) => {
    const request = settingsQueue.then(operation, operation)
    settingsQueue = request.catch(() => {})
    return request
  }

  const loadSettings = async (options = {}) => {
    const requestId = ++settingsRequestSeq
    return enqueueSettingsRequest(async () => {
      // HTTP-only Cookie 不可读，直接尝试请求
      try {
        const res = await userSettingsApi.getSettings({
          skipAuthRedirect: options.skipAuthRedirect === true,
          skipErrorToast: options.skipErrorToast !== false
        })
        if (res.code === 0 && res.data) {
          const fallbackTheme = options.preserveTheme ? getCurrentSettings().theme : 'light'
          confirmedSettings = normalizeSettings(res.data, {
            siteTitle: 'CloudStream',
            theme: fallbackTheme
          })
          settingsLoaded.value = true
          if (requestId === settingsRequestSeq) applySettings(confirmedSettings)
        } else if (requestId === settingsRequestSeq && settingsLoaded.value) {
          applySettings(confirmedSettings)
        }
        return res
      } catch (error) {
        if (requestId === settingsRequestSeq && settingsLoaded.value) applySettings(confirmedSettings)
        console.error('加载用户设置失败:', error)
        return undefined
      }
    })
  }

  const updateSettings = async (data) => {
    const intent = {}
    if (typeof data.siteTitle === 'string') intent.siteTitle = data.siteTitle.trim() || 'CloudStream'
    if (data.theme === 'dark' || data.theme === 'light') intent.theme = data.theme

    const requestId = ++settingsRequestSeq
    applySettings({ ...getCurrentSettings(), ...intent })

    return enqueueSettingsRequest(async () => {
      try {
        if (!settingsLoaded.value) {
          const current = await userSettingsApi.getSettings()
          if (current.code !== 0 || !current.data) {
            throw new Error(current.message || '加载用户设置失败')
          }
          confirmedSettings = normalizeSettings(current.data, {
            siteTitle: 'CloudStream',
            theme: 'light'
          })
          settingsLoaded.value = true
        }
        const payload = {
          ...confirmedSettings,
          ...intent
        }
        const res = await userSettingsApi.updateSettings(payload)
        if (res.code === 0) {
          confirmedSettings = normalizeSettings(res.data, payload)
          settingsLoaded.value = true
          if (requestId === settingsRequestSeq) applySettings(confirmedSettings)
        } else if (requestId === settingsRequestSeq) {
          applySettings(confirmedSettings)
        }
        return res
      } catch (error) {
        if (requestId === settingsRequestSeq) applySettings(confirmedSettings)
        console.error('更新用户设置失败:', error)
        throw error
      }
    })
  }

  const toggleTheme = async () => {
    const newTheme = isDark.value ? 'light' : 'dark'
    return updateSettings({ theme: newTheme })
  }

  const setSiteTitle = async (newTitle) => {
    const title = newTitle && newTitle.trim() ? newTitle : 'CloudStream'
    return updateSettings({ siteTitle: title })
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
