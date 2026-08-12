import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 开发时把 /api 代理到 Economy World 后端观测服务（默认 :19100）。
// 由 ECO_OBS_ADDR 控制，如后端换了端口改这里。
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5299,
    proxy: {
      '/api': {
        target: 'http://localhost:19100',
        changeOrigin: true,
      },
    },
  },
})
