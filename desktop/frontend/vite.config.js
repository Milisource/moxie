import {defineConfig} from 'vite'
import {svelte} from '@sveltejs/vite-plugin-svelte'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [svelte()],
  server: {
    fs: {
      // Allow the dev server to serve engine-colors.json (single source of
      // truth at the repo root's internal/engine/) imported by the frontend.
      // Production builds read from disk directly and are unaffected.
      allow: ['../..'],
    },
  },
})
