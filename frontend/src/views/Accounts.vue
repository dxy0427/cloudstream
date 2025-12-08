<template>
  <n-space vertical>
    <n-card>
      <n-space justify="space-between">
        <h3>云账户管理</h3>
        <n-button type="primary" @click="openModal(null)">添加账户</n-button>
      </n-space>
    </n-card>

    <n-data-table :columns="columns" :data="data" :loading="loading" />

    <n-modal v-model:show="showModal" preset="card" title="云账户配置" style="width: 600px">
      <n-form ref="formRef" :model="form" label-placement="left" label-width="120">
        <n-form-item label="名称" path="Name">
          <n-input v-model:value="form.Name" placeholder="账户备注" />
        </n-form-item>
        <n-form-item label="类型" path="Type">
          <n-radio-group v-model:value="form.Type">
            <n-radio-button value="123pan">123云盘</n-radio-button>
            <n-radio-button value="openlist">OpenList</n-radio-button>
          </n-radio-group>
        </n-form-item>

        <template v-if="form.Type === '123pan'">
          <n-form-item label="Client ID">
            <n-input v-model:value="form.ClientID" />
          </n-form-item>
          <n-form-item label="Client Secret">
            <n-input type="password" show-password-on="click" v-model:value="form.ClientSecret" />
          </n-form-item>
        </template>

        <template v-else>
          <n-form-item label="URL 地址">
            <n-input v-model:value="form.OpenListURL" placeholder="http://192.168.1.5:5244" />
          </n-form-item>
          <n-form-item label="Token">
             <n-input type="password" show-password-on="click" v-model:value="form.OpenListToken" />
          </n-form-item>
        </template>

        <n-form-item label="STRM Base URL">
           <n-input v-model:value="form.StrmBaseURL" placeholder="http://<宿主机IP>:12398" />
        </n-form-item>
        
        <n-space justify="end">
          <n-button @click="testConnection">测试连接</n-button>
          <n-button type="primary" @click="submit">保存</n-button>
        </n-space>
      </n-form>
    </n-modal>
  </n-space>
</template>

<script setup>
import { ref, reactive, onMounted, h } from 'vue'
import { NButton, NSpace, NTag, useMessage, useDialog } from 'naive-ui'
import api from '../api'

const message = useMessage()
const dialog = useDialog()

const data = ref([])
const loading = ref(false)
const showModal = ref(false)
const form = reactive({
  ID: 0, Name: '', Type: '123pan', ClientID: '', ClientSecret: '', OpenListURL: '', OpenListToken: '', StrmBaseURL: ''
})

const columns = [
  { title: 'ID', key: 'ID', width: 60 },
  { title: '名称', key: 'Name' },
  { 
    title: '类型', 
    key: 'Type',
    render(row) {
      return h(NTag, { type: row.Type === '123pan' ? 'info' : 'success' }, { default: () => row.Type })
    }
  },
  {
    title: '操作',
    key: 'actions',
    render(row) {
      return h(NSpace, null, {
        default: () => [
          h(NButton, { size: 'small', onClick: () => openModal(row) }, { default: () => '编辑' }),
          h(NButton, { size: 'small', type: 'error', onClick: () => handleDelete(row) }, { default: () => '删除' })
        ]
      })
    }
  }
]

const fetchData = async () => {
  loading.value = true
  const res = await api.get('/accounts')
  data.value = res.data || []
  loading.value = false
}

const openModal = (row) => {
  if (row) {
    Object.assign(form, row)
  } else {
    Object.assign(form, { ID: 0, Name: '', Type: '123pan', ClientID: '', ClientSecret: '', OpenListURL: '', OpenListToken: '', StrmBaseURL: '' })
  }
  showModal.value = true
}

const testConnection = async () => {
  try {
    const res = await api.post('/accounts/test', form)
    message.success(res.message)
  } catch (e) {}
}

const submit = async () => {
  try {
    if (form.ID) {
      await api.put(`/accounts/${form.ID}`, form)
    } else {
      await api.post('/accounts', form)
    }
    message.success('保存成功')
    showModal.value = false
    fetchData()
  } catch (e) {}
}

const handleDelete = (row) => {
  dialog.warning({
    title: '警告',
    content: '确定要删除此账户吗？关联的任务也将被删除。',
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      await api.delete(`/accounts/${row.ID}`)
      message.success('删除成功')
      fetchData()
    }
  })
}

onMounted(fetchData)
</script>