<template>
  <div style="height: 450px; display: flex; flex-direction: column;">
    <!-- 面包屑导航 -->
    <n-breadcrumb>
      <n-breadcrumb-item @click="loadFiles('0')">
        <n-icon><HomeOutlined /></n-icon> 根目录
      </n-breadcrumb-item>
      <n-breadcrumb-item v-for="(item, idx) in pathStack" :key="idx" @click="jumpTo(idx)">
        {{ item.name }}
      </n-breadcrumb-item>
    </n-breadcrumb>

    <!-- 文件列表容器 -->
    <div style="flex: 1; margin-top: 10px; border: 1px solid #333; border-radius: 4px; overflow: hidden; position: relative;">
      <n-spin :show="loading" style="height: 100%">
        <!-- 空状态 -->
        <n-empty 
          v-if="files.length === 0 && !loading" 
          description="空目录" 
          style="position: absolute; top: 50%; left: 50%; transform: translate(-50%, -50%);" 
        />
        
        <!-- 虚拟滚动列表 -->
        <n-virtual-list
          v-else
          style="height: 100%"
          :item-size="46"
          :items="files"
          item-resizable
        >
          <template #default="{ item }">
            <div class="file-row" @click="handleClick(item)">
              <!-- 图标 -->
              <div class="icon-wrapper">
                <n-icon v-if="item.type === 1" color="#f0a020" size="18"><FolderOutlined /></n-icon>
                <n-icon v-else color="#888" size="18"><FileOutlined /></n-icon>
              </div>

              <!-- 文件名 -->
              <div class="name-wrapper">
                {{ item.filename }}
              </div>

              <!-- 操作按钮 (仅文件夹显示选择) -->
              <div class="action-wrapper" v-if="item.type === 1">
                <n-button size="tiny" secondary type="primary" @click.stop="$emit('select', item.fileId)">
                  选择此目录
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
const pathStack = ref([]) // 存储路径栈 {id, name}

// 加载文件列表
const loadFiles = async (parentId) => {
  if (!props.accountId) return
  loading.value = true
  try {
    // 关键修复：对 parentId 进行 URL 编码，防止 OpenList 路径包含特殊字符导致 400 错误
    const res = await api.get(`/cloud/files?accountId=${props.accountId}&parentFileId=${encodeURIComponent(parentId)}`)
    files.value = res.data.fileList || []
    
    // 如果回到根目录，清空栈
    if(parentId === '0' || parentId === '/' || parentId === '') {
      pathStack.value = []
    }
  } finally {
    loading.value = false
  }
}

// 监听账户ID变化，自动加载根目录
watch(() => props.accountId, (val) => {
  if(val) {
    pathStack.value = []
    loadFiles('0')
  }
}, { immediate: true })

// 点击行逻辑
const handleClick = (file) => {
  if (file.type === 1) { // 如果是文件夹，进入下一级
    pathStack.value.push({ id: file.fileId, name: file.filename })
    loadFiles(file.fileId)
  }
}

// 面包屑跳转
const jumpTo = (idx) => {
  const target = pathStack.value[idx]
  pathStack.value = pathStack.value.slice(0, idx + 1)
  loadFiles(target.id)
}
</script>

<style scoped>
/* 列表行样式 */
.file-row {
  height: 46px;
  display: flex;
  align-items: center;
  padding: 0 12px;
  cursor: pointer;
  border-bottom: 1px solid rgba(255, 255, 255, 0.09);
  transition: background-color 0.2s;
  box-sizing: border-box;
}

/* 适配亮色模式的边框颜色 */
:root:not(.dark) .file-row {
  border-bottom: 1px solid #eee;
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
}

.action-wrapper {
  margin-left: 10px;
}
</style>