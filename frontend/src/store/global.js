import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useGlobalStore = defineStore('global', () => {
  // 优先从 localStorage 读取标题，默认值为 CloudStream
  const savedTitle = localStorage.getItem('site_title')
  const siteTitle = ref(savedTitle || 'CloudStream')
  
  // 默认深色模式
  const isDark = ref(localStorage.getItem('theme') !== 'light')

  const toggleTheme = () => {
    isDark.value = !isDark.value
    localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
  }

  const setSiteTitle = (newTitle) => {
    siteTitle.value = newTitle
    localStorage.setItem('site_title', newTitle)
    // 动态修改浏览器标签页标题
    document.title = newTitle
  }

  // 初始化时设置一下文档标题
  document.title = siteTitle.value

  return { siteTitle, isDark, toggleTheme, setSiteTitle }
})