<template>
 <n-config-provider :theme="theme">
  <n-global-style />
  <!-- 核心修复：增加加载条 Provider -->
  <n-loading-bar-provider>
    <n-message-provider>
     <n-dialog-provider>
      <router-view />
     </n-dialog-provider>
    </n-message-provider>
  </n-loading-bar-provider>
 </n-config-provider>
</template>

<script setup>
import { computed } from 'vue'
import { darkTheme } from 'naive-ui'
import { useGlobalStore } from './store/global'

const store = useGlobalStore()
const theme = computed(() => store.isDark ? darkTheme : null)
</script>