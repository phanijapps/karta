import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [svelte(), tailwindcss()],
  server: {
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
    // Same-origin daemon access in development: zbotd (loopback) serves
    // the contract; the SPA never dials providers directly.
    proxy: {
      '/api': { target: process.env.ZBOTD_URL ?? 'http://127.0.0.1:8787' },
      '/healthz': { target: process.env.ZBOTD_URL ?? 'http://127.0.0.1:8787' },
    },
  },
  preview: {
    proxy: {
      '/api': { target: process.env.ZBOTD_URL ?? 'http://127.0.0.1:8787' },
      '/healthz': { target: process.env.ZBOTD_URL ?? 'http://127.0.0.1:8787' },
    },
  },
  build: { outDir: 'dist' },
});
