import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The dashboard is built into ../dist, which the Go package embeds via
// //go:embed. `npm run dev` proxies the API to a locally running `csdd web`.
export default defineConfig({
  plugins: [react()],
  base: '/',
  build: {
    outDir: '../dist',
    emptyOutDir: true,
    // Monaco is intentionally large; don't warn about the bundle size.
    chunkSizeWarningLimit: 5000,
  },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:7777',
    },
  },
})
