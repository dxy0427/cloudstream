<template>
  <div class="browser-container">
    <!-- 顶部面包屑 -->
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

    <!-- 列表区域容器 -->
    <div class="list-wrapper">
      <n-spin :show="loading" style="height: 100%">
        <!-- 空状态 -->
        <div v-if="files.length === 0 && !loading" class="empty-state">
          <n-empty description="此目录为空" />
        </div>

        <!-- 核心修复：原生滚动容器 -->
        <!-- 这种写法兼容性最强，手机电脑都能滚 -->
        <div v-else class="scroll-container">
          <div 
            v-for="item in files" 
            :key="item.fileId" 
            class="file-row" 
            @click="handleClick(item)"
          >
            <!-- 图标 -->
            <div class="icon-wrapper">
              <n-icon v-if="item.type === 1" color="#f0a020" size="22"><FolderOutlined /></n-icon>
              <n-icon v-else color="#888" size="22"><FileOutlined /></n-icon>
            </div>

            <!-- 文件名 -->
            <div class="name-wrapper">
              {{ item.filename }}
            </div>

            <!-- 操作按钮 -->
            <div class="action-wrapper" v-if="item.type === 1">
              <n-button size="tiny" secondary type="primary" @click.stop="$emit('select', item.fileId)">
                选择
              </n-button>
            </div>
          </div>
        </div>
      </n-spin>
    </div>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { FolderOutlined, FileOutlined, HomeOutlined } from '@vicons/antd'
import { NIcon, NButton, NBreadcrumb, NBreadcrumbItem, NSpin, NEmpty } from 'naive-ui'
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
    // 增加 encodeURIComponent 防止路径特殊字符报错
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
/* 1. 总容器：限制最大高度，由 Modal 决定 */
.browser-container {
  height: 60vh; /* 占据屏幕高度的 60%，保证有空间滚动 */
  display: flex;
  flex-direction: column;
}

/* 移动端稍微高一点 */
@media (max-width: 600px) {
  .browser-container {
    height: 70vh;
  }
}

.breadcrumb-area {
  padding-bottom: 12px;
  flex-shrink: 0;
  border-bottom: 1px solid rgba(128, 128, 128, 0.1);
  margin-bottom: 5px;
}

/* 2. 列表外层封装 */
.list-wrapper {
  flex: 1; /* 占满剩余空间 */
  position: relative;
  overflow: hidden; /* 防止溢出 */
  background-color: rgba(0, 0, 0, 0.02);
  border-radius: 4px;
}

/* 3. 滚动容器 (核心修复) */
.scroll-container {
  height: 100%;           /* 填满父容器 */
  overflow-y: auto;       /* 开启垂直滚动 */
  overflow-x: hidden;     /* 禁止水平滚动 */
  -webkit-overflow-scrolling: touch; /* iOS 惯性滚动支持 */
}

.empty-state {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* 4. 列表项样式 */
.file-row {
  display: flex;
  align-items: center;
  padding: 12px 10px; /* 增加点击区域，方便手指点击 */
  cursor: pointer;
  border-bottom: 1px solid rgba(128, 128, 128, 0.1);
  transition: background-color 0.2s;
}

.file-row:active {
  background-color: rgba(128, 128, 128, 0.2);
}

/* PC 端 Hover 效果 */
@media (hover: hover) {
  .file-row:hover {
    background-color: rgba(128, 128, 128, 0.1);
  }
}

.icon-wrapper {
  display: flex;
  align-items: center;
  margin-right: 12px;
  flex-shrink: 0;
}

.name-wrapper {
  flex: 1;
  font-size: 14px;
  word-break: break-all; /* 防止长文件名撑破布局 */
  line-height: 1.4;
}

.action-wrapper {
  margin-left: 10px;
  flex-shrink: 0;
}
</style>