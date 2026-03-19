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
import { computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { darkTheme } from 'naive-ui'
import { useGlobalStore } from './store/global'

const store = useGlobalStore()
const route = useRoute()
const theme = computed(() => route.meta?.noAuth ? null : (store.isDark ? darkTheme : null))

onMounted(() => {
  store.loadSettings()
})
</script>
