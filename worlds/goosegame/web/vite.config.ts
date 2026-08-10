import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 开发时把 /api 代理到 GooseGame 后端观测服务（默认 :19090）。
// 由 GOOSE_OBS_ADDR 控制，如后端换了端口改这里。
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5199,
    proxy: {
      '/api': {
        target: 'http://localhost:19090',
        changeOrigin: true,
      },
    },
  },
})
