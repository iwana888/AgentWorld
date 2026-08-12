import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 开发时把 /api 代理到 GooseGame 后端观测服务（默认 :19090）。
// 由 GOOSE_OBS_ADDR 控制，如后端换了端口改这里。
// 构建产物输出到 ../webstatic/dist（相对 web/ 目录），由 Go 通过 //go:embed 打进二进制，
// 这样整个世界只需一个 exe / 一个容器即可跑（前端与 API 同源，无需单独跑 vite）。
// base 用相对路径 './'，保证部署在任意子路径下也能正确加载资源。
export default defineConfig({
  plugins: [vue()],
  base: './',
  server: {
    port: 5199,
    proxy: {
      '/api': {
        target: 'http://localhost:19090',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: '../webstatic/dist',
    emptyOutDir: true,
  },
})
