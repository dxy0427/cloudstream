<template>
  <!-- 1. 外层容器：固定高度 500px，确保模态框内有足够空间 -->
  <div class="browser-container">
    <!-- 面包屑导航 -->
    <div class="breadcrumb-area">
      <n-breadcrumb>
        <n-breadcrumb-item @click="loadFiles('0')">
          <n-icon><HomeOutlined /></n-icon> 根目录
        </n-breadcrumb-item>
        <n-breadcrumb-item v-for="(item, idx) in pathStack" :key="idx" @click="jumpTo(idx)">
          {{ item.name }}
        </n-breadcrumb-item>
      </n-breadcrumb>
    </div>

    <!-- 2. 列表区域：占据剩余空间，且必须有 hidden 防止溢出 -->
    <div class="list-area">
      <n-spin :show="loading" style="height: 100%">
        <!-- 空状态 -->
        <n-empty 
          v-if="files.length === 0 && !loading" 
          description="此目录为空" 
          style="padding-top: 60px;" 
        />
        
        <!-- 3. 虚拟列表：核心修复 -->
        <!-- item-size: 每一行的高度，必须与下方 css .file-row 高度一致 -->
        <!-- style="height: 100%": 必须填满父容器，滚动条才会出现 -->
        <n-virtual-list
          v-else
          style="height: 100%; max-height: 100%;"
          :item-size="46"
          :items="files"
          item-resizable
        >
          <template #default="{ item }">
            <div class="file-row" @click="handleClick(item)">
              <div class="icon-wrapper">
                <n-icon v-if="item.type === 1" color="#f0a020" size="20"><FolderOutlined /></n-icon>
                <n-icon v-else color="#888" size="20"><FileOutlined /></n-icon>
              </div>

              <div class="name-wrapper" :title="item.filename">
                {{ item.filename }}
              </div>

              <div class="action-wrapper" v-if="item.type === 1">
                <n-button size="tiny" secondary type="primary" @click.stop="$emit('select', item.fileId)">
                  选择
                </n-button>
              </div>
            </div>
          </template>
        </n-virtual-list>
      </n-spin>
    </div>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { FolderOutlined, FileOutlined, HomeOutlined } from '@vicons/antd'
import { NIcon, NButton, NBreadcrumb, NBreadcrumbItem, NSpin, NVirtualList, NEmpty } from 'naive-ui'
import api from '../api'

const props = defineProps(['accountId'])
const emit = defineEmits(['select'])

const loading = ref(false)
const files = ref([])
const pathStack = ref([])

const loadFiles = async (parentId) => {
  if (!props.accountId) return
  loading.value = true
  try {
    const res = await api.get(`/cloud/files?accountId=${props.accountId}&parentFileId=${encodeURIComponent(parentId)}`)
    files.value = res.data.fileList || []
    if(parentId === '0' || parentId === '/' || parentId === '') {
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
  if (file.type === 1) { 
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

<style scoped>
/* 使用 Flex 布局强制撑开高度 */
.browser-container {
  height: 500px; /* 总高度固定 */
  display: flex;
  flex-direction: column;
}

.breadcrumb-area {
  padding-bottom: 10px;
  flex-shrink: 0; /* 防止面包屑被压缩 */
}

.list-area {
  flex: 1; /* 自动占满剩余空间 */
  border: 1px solid rgba(128, 128, 128, 0.2);
  border-radius: 4px;
  overflow: hidden; /* 关键：防止内容溢出父容器 */
  position: relative;
  background-color: rgba(0, 0, 0, 0.02);
}

/* 列表行样式 */
.file-row {
  height: 46px; /* 必须与 item-size 一致 */
  display: flex;
  align-items: center;
  padding: 0 12px;
  cursor: pointer;
  border-bottom: 1px solid rgba(128, 128, 128, 0.1);
  box-sizing: border-box;
}

.file-row:hover {
  background-color: rgba(128, 128, 128, 0.1);
}

.icon-wrapper {
  display: flex;
  align-items: center;
  margin-right: 12px;
}

.name-wrapper {
  flex: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 14px;
  user-select: none; /* 防止拖动滚动条时意外选中文字 */
}

.action-wrapper {
  margin-left: 10px;
}
</style>