<template>
 <n-config-provider :theme="theme">
  <n-global-style />
  <n-message-provider>
   <n-dialog-provider>
    <router-view />
   </n-dialog-provider>
  </n-message-provider>
 </n-config-provider>
</template>

<script setup>
import { computed } from 'vue'
import { darkTheme } from 'naive-ui'
import { useGlobalStore } from './store/global'

const store = useGlobalStore()

// 如果是深色模式，传 darkTheme；浅色模式传 null
const theme = computed(() => store.isDark ? darkTheme : null)
</script>