<template>
  <div style="height: 400px; display: flex; flex-direction: column;">
    <!-- 面包屑保持不变 -->
    <n-breadcrumb>...</n-breadcrumb>

    <div style="flex: 1; margin-top: 10px; border: 1px solid #333; height: 0;"> <!-- height:0 是为了让 flex 生效 -->
      <n-spin :show="loading" style="height: 100%">
        <n-empty v-if="files.length === 0 && !loading" description="空目录" style="padding-top: 50px" />
        
        <!-- 核心修改：使用虚拟列表 -->
        <n-virtual-list
          v-else
          :item-size="42" 
          :items="files"
          item-resizable
          style="height: 100%"
        >
          <template #default="{ item }">
            <div 
              class="file-item" 
              @click="handleClick(item)"
              style="height: 42px; display: flex; align-items: center; padding: 0 10px; cursor: pointer; border-bottom: 1px solid #333;"
            >
              <n-icon v-if="item.type === 1" color="#f0a020" style="margin-right: 8px"><FolderOutlined /></n-icon>
              <n-icon v-else style="margin-right: 8px"><FileOutlined /></n-icon>
              
              <span style="flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">
                {{ item.filename }}
              </span>

              <n-button v-if="item.type === 1" size="tiny" secondary @click.stop="$emit('select', item.fileId)">
                选择
              </n-button>
            </div>
          </template>
        </n-virtual-list>
      </n-spin>
    </div>
  </div>
</template>

<style scoped>
.file-item:hover {
  background-color: rgba(255, 255, 255, 0.1); /* 简单的 Hover 效果 */
}
</style>