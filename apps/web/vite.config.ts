import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react-swc';
import { fileURLToPath, URL } from 'node:url';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
      '@api': fileURLToPath(new URL('./src/lib/api', import.meta.url)),
      '@components': fileURLToPath(new URL('./src/lib/components', import.meta.url)),
      '@stores': fileURLToPath(new URL('./src/lib/stores', import.meta.url)),
      '@utils': fileURLToPath(new URL('./src/lib/utils', import.meta.url)),
    },
  },
  build: {
    chunkSizeWarningLimit: 2600,
  },
  server: {
    host: '0.0.0.0',
    port: 55173,
    proxy: {
      // Auth cookies (of_session, of_refresh) flow through the proxy
      // back to the browser unchanged — vite's default proxy forwards
      // Set-Cookie verbatim and the browser binds the cookie to the
      // dev host (localhost:55173). `changeOrigin: true` rewrites the
      // Host header on the way out; the cookie domain stays host-only
      // because identity-federation-service does not set an explicit
      // Domain attribute in dev.
      '/api/v1/data-connection/egress-policies': {
        target: 'http://127.0.0.1:50119',
        changeOrigin: true,
      },
      '/api/v1/data-connection': {
        target: 'http://127.0.0.1:50088',
        changeOrigin: true,
      },
      '/api/v1/auth': {
        target: 'http://127.0.0.1:50088',
        changeOrigin: true,
      },
      '/api/v1/users/me': {
        target: 'http://127.0.0.1:50088',
        changeOrigin: true,
      },
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
        ws: true,
      },
    },
  },
});
