<template>
 <div style="height: 400px; display: flex; flex-direction: column;">
  <n-breadcrumb>
   <n-breadcrumb-item @click="navigateToRoot">
    <n-icon><HomeOutlined /></n-icon> 根目录
   </n-breadcrumb-item>
   <n-breadcrumb-item v-for="(item, idx) in pathStack" :key="idx" @click="jumpTo(idx)">
    {{ item.name }}
   </n-breadcrumb-item>
  </n-breadcrumb>
	<n-button size="small" secondary style="margin-top: 10px; align-self: flex-start" @click="selectCurrent">选择当前目录</n-button>

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
import { ref, watch, onUnmounted } from 'vue'
import { FolderOutlined, FileOutlined, HomeOutlined } from '@vicons/antd'
import { NIcon } from 'naive-ui'
import api from '../api'

const props = defineProps(['accountId'])
const emit = defineEmits(['select'])

const loading = ref(false)
const files = ref([])
const pathStack = ref([]) // {id, name}
let requestSeq = 0
let committedAccountId = null
let committedFiles = []
let committedPath = []

const loadFiles = async (parentId, nextPath) => {
 if (!props.accountId) return false
 const requestId = ++requestSeq
 const accountId = props.accountId
 loading.value = true
 try {
  // 对 parentId 进行 URL 编码，支持 OpenList 路径包含 / 等字符
  const res = await api.get(`/cloud/files?accountId=${accountId}&parentFileId=${encodeURIComponent(parentId)}`)
  if (requestId !== requestSeq || accountId !== props.accountId) return false
  if (res?.code !== 0) throw new Error(res?.message || '目录加载失败')
  files.value = res.data?.fileList || []
  pathStack.value = nextPath
  committedAccountId = accountId
  committedFiles = files.value
  committedPath = nextPath
  return true
 } catch {
  if (requestId !== requestSeq || accountId !== props.accountId) return false
  files.value = committedAccountId === accountId ? committedFiles : []
  pathStack.value = committedAccountId === accountId ? committedPath : []
  return false
 } finally {
  if (requestId === requestSeq) loading.value = false
 }
}

watch(() => props.accountId, (val) => {
 if (val) {
  requestSeq++
  committedAccountId = val
  committedFiles = []
  committedPath = []
  files.value = []
  pathStack.value = []
  loadFiles('0', [])
 } else {
  requestSeq++
  committedAccountId = null
  committedFiles = []
  committedPath = []
  loading.value = false
  files.value = []
  pathStack.value = []
 }
}, { immediate: true })

const handleClick = async (file) => {
 if (file.type === 1) { // Directory
  await loadFiles(file.fileId, [...pathStack.value, { id: file.fileId, name: file.filename }])
 }
}

const jumpTo = async (idx) => {
 const target = pathStack.value[idx]
 if (!target) return
 await loadFiles(target.id, pathStack.value.slice(0, idx + 1))
}

const navigateToRoot = () => loadFiles('0', [])

const selectCurrent = () => {
 const current = pathStack.value[pathStack.value.length - 1]
 emit('select', current?.id || '0')
}

onUnmounted(() => {
 requestSeq++
})
</script>
