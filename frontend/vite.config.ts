import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'
import { readDeployment } from './scripts/deployment.mjs'

const { basePath } = readDeployment()
const prefix = basePath.slice(0, -1)
const stripPrefix = (url: string) => url.slice(prefix.length) || '/'


// https://vitejs.dev/config/
export default defineConfig({
  base: basePath,
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 3030,
    proxy: {
      [`${basePath}api`]: {
        rewrite: stripPrefix,
        target: process.env.VITE_API_TARGET || 'http://localhost:8080', // Go backend (studio-api)
        changeOrigin: true,
        // SSE must not be buffered by the dev proxy, otherwise stream
        // frames arrive only when the connection closes (client shows
        // the reply only after refresh).
        configure(proxy) {
          proxy.on('proxyRes', (proxyRes, req) => {
            if (req.url?.includes('/stream')) {
              proxyRes.headers['cache-control'] = 'no-cache, no-transform';
              proxyRes.headers['x-accel-buffering'] = 'no';
            }
          })
        },
      },
      [`${basePath}ws`]: {
        rewrite: stripPrefix,
        target: process.env.VITE_WS_TARGET || 'ws://localhost:8080',
        ws: true,
        changeOrigin: true,
      },
    }
  }
})
