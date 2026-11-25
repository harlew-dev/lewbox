import { spawnSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { defineConfig } from '@playwright/test'
import { adminEmail, adminPassword } from './fixtures'

const root = path.resolve(import.meta.dirname, '../..')
const webRoot = path.resolve(import.meta.dirname, '..')
const binary = path.join(root, 'bin', 'lewbox')

// Playwright loads this config in its worker processes too. The first load
// makes the run's directory and port; the workers reuse them through the
// environment instead of claiming another port.
function setup(): { tmpdir: string; baseURL: string; port: number } {
  const existing = process.env.LEWBOX_E2E_TMPDIR
  const existingBaseURL = process.env.LEWBOX_E2E_BASE_URL
  if (existing && existingBaseURL) {
    return { tmpdir: existing, baseURL: existingBaseURL, port: Number(new URL(existingBaseURL).port) }
  }

  const tmpdir = fs.mkdtempSync(path.join(os.tmpdir(), 'lewbox-e2e-'))
  const { port, lock } = claimPort()
  const baseURL = `http://127.0.0.1:${port}`
  process.env.LEWBOX_E2E_TMPDIR = tmpdir
  process.env.LEWBOX_E2E_BASE_URL = baseURL
  // teardown.ts reads these to remove the run's temporary files.
  process.env.LEWBOX_E2E_LOCK = lock
  return { tmpdir, baseURL, port }
}

const { tmpdir, baseURL, port } = setup()

// ephemeralPort asks the kernel for a free loopback port, then closes it
// again. The caller claims it with a lock before using it.
function ephemeralPort(): number {
  const script =
    "const s=require('node:net').createServer();s.listen(0,'127.0.0.1',()=>{console.log(s.address().port);s.close()})"
  const result = spawnSync(process.execPath, ['-e', script], { encoding: 'utf8' })
  const picked = Number(result.stdout.trim())
  if (!Number.isInteger(picked) || picked <= 0) {
    throw new Error(
      `could not pick an ephemeral port (status ${result.status}, error ${result.error?.message ?? 'none'}): ${result.stderr.trim()}`,
    )
  }
  return picked
}

function running(pid: number): boolean {
  try {
    process.kill(pid, 0)
    return true
  } catch {
    return false
  }
}

function claimPort(): { port: number; lock: string } {
  for (let attempt = 0; attempt < 20; attempt++) {
    const port = ephemeralPort()
    const lock = path.join(os.tmpdir(), `lewbox-e2e-${port}.lock`)
    try {
      fs.writeFileSync(lock, String(process.pid), { flag: 'wx' })
      return { port, lock }
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== 'EEXIST') throw error
      const holder = Number(fs.readFileSync(lock, 'utf8'))
      // A crashed run leaves its lock behind; take it over only when the
      // process that wrote it is gone.
      if (!holder || !running(holder)) fs.rmSync(lock, { force: true })
    }
  }
  throw new Error('could not reserve a free port for the e2e server')
}

const command = [
  `"${binary}" serve`,
  `--listen 127.0.0.1:${port}`,
  '--check-interval 0',
  '--sync-interval 0',
  '--archive-interval 0',
].join(' ')

export default defineConfig({
  testDir: '.',
  testMatch: '*.spec.ts',
  // The suite is small and shares one server; one worker keeps it quick and
  // deterministic.
  workers: 1,
  fullyParallel: false,
  forbidOnly: process.env.CI === 'true',
  retries: 0,
  timeout: 30_000,
  expect: { timeout: 5_000 },
  reporter: [['list'], ['html', { outputFolder: path.join(webRoot, 'playwright-report'), open: 'never' }]],
  outputDir: path.join(webRoot, 'test-results'),
  globalTeardown: './teardown.ts',
  use: {
    baseURL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
  webServer: {
    command,
    cwd: root,
    url: `${baseURL}/v0/health`,
    // Never reuse a server: it may be another run's, from other code.
    reuseExistingServer: false,
    timeout: 120_000,
    stdout: 'pipe',
    stderr: 'pipe',
    env: {
      LEWBOX_DB: path.join(tmpdir, 'lewbox.db'),
      LEWBOX_BOOTSTRAP_ADMIN_EMAIL: adminEmail,
      LEWBOX_BOOTSTRAP_ADMIN_PASSWORD: adminPassword,
      // Keep every other file the server writes inside this run's directory.
      XDG_CONFIG_HOME: path.join(tmpdir, 'config'),
      XDG_CACHE_HOME: path.join(tmpdir, 'cache'),
      XDG_DATA_HOME: path.join(tmpdir, 'data'),
    },
  },
})
