import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

// 本地调试默认复用线上后端；如需调试本机后端，在 web/.env.local 中设置
// VITE_API_PROXY_TARGET=http://127.0.0.1:8888 即可，无需修改源码。
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const apiProxyTarget = env.VITE_API_PROXY_TARGET || 'https://doc.inkflowai.top'

  return {
    plugins: [vue()],
    server: {
      proxy: {
        // 保持浏览器侧同源：Cookie、SSE 与 WebSocket 都通过 Vite 转发。
        '/api': {
          target: apiProxyTarget,
          changeOrigin: true,
          ws: true,
        },
        '/hf-mirror': {
          target: apiProxyTarget,
          changeOrigin: true,
        },
      },
    },
  }
})
