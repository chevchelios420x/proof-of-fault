import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// "npm run dev": API calls go to a locally running service.
export default defineConfig({
  plugins: [svelte()],
  server: { proxy: { '/api': { target: process.env.POF_API || 'http://127.0.0.1:8443', secure: false } } },
})
