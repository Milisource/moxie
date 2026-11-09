import {defineConfig} from 'vite'
import {svelte} from '@sveltejs/vite-plugin-svelte'
import {writeFileSync} from 'node:fs'
import {fileURLToPath} from 'node:url'

// vite's default emptyOutDir wipes frontend/dist on every build, deleting the
// committed .gitkeep that //go:embed all:frontend/dist (desktop/main.go) needs
// on a fresh clone. Recreate it once the bundle is written.
function keepGitkeep() {
  const placeholder =
    '# Keeps desktop/frontend/dist present for //go:embed all:frontend/dist on a fresh clone.\n'
  return {
    name: 'keep-gitkeep',
    closeBundle() {
      writeFileSync(fileURLToPath(new URL('./dist/.gitkeep', import.meta.url)), placeholder)
    },
  }
}

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [svelte(), keepGitkeep()],
  server: {
    fs: {
      // Allow the dev server to serve engine-colors.json (single source of
      // truth at the repo root's internal/engine/) imported by the frontend.
      // Production builds read from disk directly and are unaffected.
      allow: ['../..'],
    },
  },
})
