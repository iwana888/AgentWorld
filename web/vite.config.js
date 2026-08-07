import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 开发时 Vite 在 5173 端口，/api 代理到 Go 后端 18080
// 构建产物输出到 ../webstatic/dist（相对 web/ 目录），由 Go 通过 embed 打包进二进制
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: { '/api': 'http://localhost:18080' }
  },
  build: { outDir: '../webstatic/dist', emptyOutDir: true }
})
