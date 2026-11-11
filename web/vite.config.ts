import fs from 'node:fs'
import path from 'node:path'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vitest/config'
import type { Plugin } from 'vite'

// emptyOutDir removes web/dist entirely, including the .gitkeep that keeps the
// folder embeddable before the first build. Put it back after the bundle is
// written.
function keepDistFolder(): Plugin {
  return {
    name: 'lewbox-keep-dist-gitkeep',
    apply: 'build',
    closeBundle() {
      const dir = path.resolve(process.cwd(), 'dist')
      fs.mkdirSync(dir, { recursive: true })
      fs.writeFileSync(path.join(dir, '.gitkeep'), '')
    },
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss(), keepDistFolder()],
  resolve: {
    dedupe: ['react', 'react-dom'],
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  base: '/',
  server: {
    port: 5173,
    proxy: {
      '/v0': 'http://127.0.0.1:8080',
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    css: false,
  },
})
