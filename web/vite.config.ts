import fs from 'node:fs'
import path from 'node:path'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { configDefaults, defineConfig } from 'vitest/config'
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
    rollupOptions: {
      output: {
        // React and the router change less often than the app, so they get a
        // chunk of their own. Markdown is already split off with the lazy docs
        // page in src/app.tsx; listing it here would pull its shared
        // dependencies into a chunk the entry then loads eagerly.
        manualChunks(id) {
          if (/[\\/]node_modules[\\/](react|react-dom|scheduler|react-router)[\\/]/.test(id)) return 'react'
        },
      },
    },
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
    // The Playwright suite under e2e/ is not a jsdom test.
    exclude: [...configDefaults.exclude, 'e2e/**'],
    // Above the 5 s findBy timeout in src/test/setup.ts.
    testTimeout: 15_000,
  },
})
