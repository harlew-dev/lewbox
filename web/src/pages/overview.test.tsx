import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AppLinkProvider } from '../components/app-link'
import { ConsoleLayout } from '../components/console-layout'
import { AuthProvider } from '../lib/auth'
import { OverviewPage } from './overview'

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const user = {
  id: '01a1cb34-659b-74a8-983d-085c0383e0a6',
  email: 'admin@example.com',
  instance_admin: true,
  disabled: false,
  created_at: '2026-11-11T15:22:51.1Z',
}

beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url === '/v0/me') return json(200, { user, via: 'session' })
      if (url === '/v0/clusters') {
        return json(200, {
          data: [
            {
              id: '01a1cb34-29a5-74c3-9691-5394d021b0f2',
              slug: 'eu-west',
              name: 'eu-west',
              admin_url: 'http://10.0.0.5:21212',
              endpoint_url: 'http://10.0.0.5:21212',
              runtime: 'docker',
              admin_token_last4: '9f3a',
              status: 'ready',
              version: 'v0.7.0',
              nodes: 1,
              checked_at: '2026-11-11T15:23:01.2Z',
              created_at: '2026-11-11T15:22:35.8Z',
            },
          ],
        })
      }
      if (url === '/v0/nodes') return json(200, { data: [] })
      throw new Error(`unexpected request: ${url}`)
    }),
  )
})

describe('OverviewPage', () => {
  it('renders the console and the cluster table', async () => {
    render(
      <MemoryRouter initialEntries={['/']}>
        <AppLinkProvider>
          <AuthProvider>
            <Routes>
              <Route
                path="*"
                element={
                  <ConsoleLayout>
                    <OverviewPage />
                  </ConsoleLayout>
                }
              />
            </Routes>
          </AuthProvider>
        </AppLinkProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('heading', { name: 'Overview' })).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Clusters' })).toBeInTheDocument()
    // The cluster's row links to its page.
    const links = screen.getAllByRole('link', { name: /eu-west/ })
    expect(links[0]).toHaveAttribute('href', '/servers/clusters/eu-west')
    expect(screen.getByText('1 ready')).toBeInTheDocument()
    expect(screen.getByText('Every cluster and node is healthy')).toBeInTheDocument()
  })
})
