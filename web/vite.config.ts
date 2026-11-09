import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      // 开发期转发到本地 server（make run）；内嵌部署时同源无需代理
      '/api': 'http://localhost:8080',
      '/health': 'http://localhost:8080',
    },
  },
})
