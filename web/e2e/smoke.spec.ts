import { expect, test, type Page } from '@playwright/test'
import { adminEmail, adminPassword } from './fixtures'

// Every page error, console error and CSP violation fails the test that saw
// it. They are collected for the page under test and asserted after it.
declare global {
  interface Window {
    __lewboxCspViolations?: string[]
  }
}

const failures: string[] = []

test.beforeEach(async ({ page }) => {
  failures.length = 0
  await page.addInitScript(() => {
    window.addEventListener('securitypolicyviolation', (event) => {
      const list = (window.__lewboxCspViolations ??= [])
      list.push(`${event.violatedDirective}: blocked ${event.blockedURI || 'inline'}`)
    })
  })
  page.on('pageerror', (error) => failures.push(`page error: ${error.message}`))
  page.on('console', (message) => {
    if (message.type() !== 'error') return
    // A signed-out page probes /v0/profile and gets a 401; Chromium logs the
    // failed request as a console error. Every other error is a failure.
    const expectedProbe =
      message.text() === 'Failed to load resource: the server responded with a status of 401 (Unauthorized)' &&
      new URL(page.url()).pathname === '/login'
    if (expectedProbe) return
    failures.push(`console error: ${message.text()}`)
  })
})

test.afterEach(async ({ page }) => {
  const violations = await page
    .evaluate(() => window.__lewboxCspViolations ?? [])
    .catch(() => [] as string[])
  expect(failures.concat(violations)).toEqual([])
})

async function signIn(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(adminEmail)
  await page.getByLabel('Password', { exact: true }).fill(adminPassword)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL(/\/admin$/)
}

test('signs in and renders the admin pages', async ({ page }) => {
  await signIn(page)
  await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible()

  const pages = [
    ['/admin/workspaces', 'Workspaces'],
    ['/admin/users', 'Users'],
    ['/admin/plans', 'Plans'],
    ['/admin/events', 'Audit trail'],
    ['/admin/settings', 'Settings'],
  ] as const
  for (const [path, heading] of pages) {
    await page.goto(path)
    await expect(page.getByRole('heading', { name: heading, exact: true })).toBeVisible()
  }
})

test('creates a workspace and opens its portal home', async ({ page }) => {
  await signIn(page)
  await page.goto('/admin/workspaces')

  await page.getByRole('button', { name: 'New workspace' }).click()
  const dialog = page.getByRole('dialog', { name: 'New workspace' })
  await expect(dialog).toBeVisible()

  const name = `Smoke ${Date.now()}`
  await dialog.getByLabel('Owner email').fill(adminEmail)
  await dialog.getByLabel('Workspace name').fill(name)
  await dialog.getByRole('button', { name: 'Create workspace' }).click()

  // The dialog opens the new workspace's console page, headed by its name.
  await expect(page).toHaveURL(/\/admin\/workspaces\/[a-z0-9-]+$/)
  await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()

  await page.goto('/admin/workspaces')
  await expect(page.getByText(name, { exact: true })).toBeVisible()

  // A fresh load reads the new membership, so the portal shows the workspace.
  await page.goto('/')
  await expect(page.getByRole('heading', { name, exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'No region yet', exact: true })).toBeVisible()
})

test('signs out from the account menu', async ({ page }) => {
  await signIn(page)
  await page.getByText(adminEmail, { exact: true }).first().click()
  await page.getByRole('menuitem', { name: 'Sign out' }).click()
  await expect(page).toHaveURL(/\/login(\?.*)?$/)
  await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible()
})
