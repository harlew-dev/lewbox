// Regenerate the README image (docs/screenshot.png): the operator dashboard in
// dark mode, framed as a window cropped by the right and bottom edges, like
// lewui's README image. The console needs real data, so this signs in to a
// running orchestrator: by default the seeded dev cluster (deploy/dev-cluster:
// ./dev.sh up, ./dev.sh cluster, ./dev.sh seed). Then run `npm run
// screenshot` here.
//
// LEWBOX_SCREENSHOT_URL, _EMAIL and _PASSWORD point it elsewhere, and
// LEWBOX_SCREENSHOT_PATH picks the page (default /admin, the operator dashboard).
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from '@playwright/test'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..')
const output = join(root, 'docs', 'screenshot.png')

const base = process.env.LEWBOX_SCREENSHOT_URL ?? 'http://127.0.0.1:8080'
const email = process.env.LEWBOX_SCREENSHOT_EMAIL ?? 'admin@lewbox.dev'
const password = process.env.LEWBOX_SCREENSHOT_PASSWORD ?? 'dev-password-123'
const path = process.env.LEWBOX_SCREENSHOT_PATH ?? '/admin'

// Captured at 2x, so 1200x630 CSS px becomes 2400x1260.
const page = { width: 1200, height: 630 }
const windowOffset = { left: 72, top: 76 }
// The console renders at a desktop size and the window crops it.
const app = { width: 1440, height: 900 }

const browser = await chromium.launch()
try {
  const context = await browser.newContext({
    viewport: page,
    deviceScaleFactor: 2,
    colorScheme: 'dark',
    reducedMotion: 'reduce',
  })

  // Sign in through the API, so the session cookie is set for the origin the
  // framed console loads from.
  const signIn = await context.request.post(new URL('/v0/auth/session', base).href, {
    data: { email, password },
    headers: { Origin: new URL(base).origin },
  })
  if (!signIn.ok()) {
    throw new Error(`Sign-in to ${base} failed with HTTP ${signIn.status()}: is the dev cluster up and seeded?`)
  }

  // The console refuses to be framed (frame-ancestors 'none'), so capture it
  // at desktop size first, then place that image in the window frame.
  const appTab = await context.newPage()
  await appTab.setViewportSize(app)
  await appTab.goto(new URL(path, base).href)
  await appTab.waitForLoadState('networkidle')
  await appTab.waitForFunction(
    () => (document.querySelector('main')?.textContent ?? '').trim().length > 0,
    undefined,
    { timeout: 15000 },
  )
  await appTab.evaluate(() => document.fonts.ready)
  // Let entrance transitions and chart layout settle.
  await appTab.waitForTimeout(1500)
  const shot = (await appTab.screenshot({ type: 'png' })).toString('base64')

  const tab = await context.newPage()
  await tab.setContent(`<!doctype html>
<html><head><style>
  * { box-sizing: border-box; }
  html, body {
    margin: 0;
    width: ${page.width}px;
    height: ${page.height}px;
    background: #2a2a2a;
    overflow: hidden;
  }
  .window {
    position: absolute;
    left: ${windowOffset.left}px;
    top: ${windowOffset.top}px;
    width: ${page.width - windowOffset.left}px;
    height: ${page.height - windowOffset.top}px;
    border-radius: 12px 0 0 0;
    overflow: hidden;
    box-shadow:
      0 0 0 1px rgba(255, 255, 255, 0.07),
      0 24px 70px rgba(0, 0, 0, 0.45);
  }
  img {
    display: block;
    width: ${app.width}px;
    height: ${app.height}px;
  }
</style></head>
<body><div class="window"><img src="data:image/png;base64,${shot}"></div></body>
</html>`)
  await tab.evaluate(() => document.querySelector('img').decode())
  await tab.screenshot({ path: output })
  await context.close()
} finally {
  await browser.close()
}

console.log(`Wrote ${output}`)
