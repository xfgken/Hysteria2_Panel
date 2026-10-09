import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// HY2 Panel 前端构建配置
//
// 产物输出到 web/dist，由 Panel 后端直接提供静态资源。
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // 单文件体积较大时不再告警（内部管理面板，不做首屏极致优化）
    chunkSizeWarningLimit: 1500,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:8080',
      '/sub': 'http://127.0.0.1:8080',
    },
  },
})