/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Dev: the Go server (go run ./cmd/portal) listens on :8080; Vite proxies
// API + health probes to it. Prod: the SPA is embedded in the Go binary
// (WU-006) and served same-origin, so no proxy exists there.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/healthz': 'http://localhost:8080',
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
  },
});
