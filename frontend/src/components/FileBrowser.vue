<template>
 <div style="height: 400px; display: flex; flex-direction: column;">
  <n-breadcrumb>
   <n-breadcrumb-item @click="loadFiles('0', '根目录')">
    <n-icon><HomeOutlined /></n-icon> 根目录
   </n-breadcrumb-item>
   <n-breadcrumb-item v-for="(item, idx) in pathStack" :key="idx" @click="jumpTo(idx)">
    {{ item.name }}
   </n-breadcrumb-item>
  </n-breadcrumb>

  <div style="flex: 1; overflow-y: auto; margin-top: 10px; border: 1px solid #333; padding: 5px;">
   <n-spin :show="loading">
    <n-list hoverable clickable>
     <n-list-item v-if="files.length === 0">
      <n-empty description="空目录" />
     </n-list-item>
     <n-list-item v-for="file in files" :key="file.fileId" @click="handleClick(file)">
       <template #prefix>
        <n-icon v-if="file.type === 1" color="#f0a020"><FolderOutlined /></n-icon>
        <n-icon v-else><FileOutlined /></n-icon>
       </template>
       {{ file.filename }}
       <template #suffix v-if="file.type === 1">
        <n-button size="tiny" secondary @click.stop="$emit('select', file.fileId)">选择</n-button>
       </template>
     </n-list-item>
    </n-list>
   </n-spin>
  </div>
 </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { FolderOutlined, FileOutlined, HomeOutlined } from '@vicons/antd'
import { NIcon } from 'naive-ui'
import api from '../api'

const props = defineProps(['accountId'])
const emit = defineEmits(['select'])

const loading = ref(false)
const files = ref([])
const pathStack = ref([]) // {id, name}

const loadFiles = async (parentId, name) => {
 if (!props.accountId) return
 loading.value = true
 try {
  // 修复：正确处理编码，后端已适配 "0" 和 "/"
  const res = await api.get(`/cloud/files?accountId=${props.accountId}&parentFileId=${encodeURIComponent(parentId)}`)
  files.value = res.data.fileList || []
  if(parentId === '0' || parentId === '/') {
    pathStack.value = []
  }
 } finally {
  loading.value = false
 }
}

watch(() => props.accountId, (val) => {
 if(val) {
  pathStack.value = []
  loadFiles('0')
 }
}, { immediate: true })

const handleClick = (file) => {
 if (file.type === 1) { // Directory
  pathStack.value.push({ id: file.fileId, name: file.filename })
  loadFiles(file.fileId)
 }
}

const jumpTo = (idx) => {
 const target = pathStack.value[idx]
 pathStack.value = pathStack.value.slice(0, idx + 1)
 loadFiles(target.id)
}
</script>