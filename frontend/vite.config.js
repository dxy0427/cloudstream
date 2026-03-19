import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import path from 'path'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { NaiveUiResolver } from 'unplugin-vue-components/resolvers'

export default defineConfig({
  plugins: [
    vue(),
    AutoImport({
      imports: [
        'vue',
        {
          'naive-ui': [
            'useDialog',
            'useMessage',
            'useNotification',
            'useLoadingBar'
          ]
        }
      ]
    }),
    Components({
      resolvers: [NaiveUiResolver()]
    })
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src')
    }
  },
  build: {
    chunkSizeWarningLimit: 900,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules/vue') || id.includes('node_modules/pinia') || id.includes('node_modules/vue-router')) {
            return 'vue-vendor'
          }
          if (id.includes('node_modules/axios')) {
            return 'axios'
          }
          if (id.includes('node_modules/naive-ui')) {
            return 'naive-ui'
          }
          if (id.includes('node_modules/@vicons')) {
            return 'icons'
          }
          if (id.includes('node_modules/vooks') || id.includes('node_modules/vdirs') || id.includes('node_modules/vueuc') || id.includes('node_modules/seemly')) {
            return 'naive-ui-helpers'
          }
        }
      }
    }
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:12398',
        changeOrigin: true
      }
    }
  }
})
