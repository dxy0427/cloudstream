import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useGlobalStore = defineStore('global', () => {
  const siteTitle = ref('CloudStream')
  const isDark = ref(localStorage.getItem('theme') !== 'light')

  const toggleTheme = () => {
    isDark.value = !isDark.value
    localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
  }

  return { siteTitle, isDark, toggleTheme }
})