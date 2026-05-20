import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Dev server proxies API + WebSocket to the Go backend. Start the backend with
// a fixed port so this target is stable:  LOUPE_ADDR=127.0.0.1:7878 ./loupe
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../internal/server/web_dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': { target: 'http://127.0.0.1:7878', ws: true },
    },
  },
})
