import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useGlobalStore = defineStore('global', () => {
  const savedTitle = localStorage.getItem('site_title')
  const siteTitle = ref(savedTitle || 'CloudStream')
  
  const isDark = ref(localStorage.getItem('theme') !== 'light')

  const toggleTheme = () => {
    isDark.value = !isDark.value
    localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
  }

  const setSiteTitle = (newTitle) => {
    // 修复：如果为空，恢复默认
    const title = newTitle && newTitle.trim() ? newTitle : 'CloudStream'
    siteTitle.value = title
    localStorage.setItem('site_title', title)
    document.title = title
  }

  document.title = siteTitle.value

  return { siteTitle, isDark, toggleTheme, setSiteTitle }
})