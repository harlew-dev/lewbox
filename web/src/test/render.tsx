import { render } from '@testing-library/react'
import type { ReactNode } from 'react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { App } from '../app'
import { vi } from 'vitest'
import { AppLinkProvider } from '../components/app-link'
import type { MeOrganization } from '../lib/api'
import { AuthProvider } from '../lib/auth'
import { InstanceProvider } from '../lib/instance'
import { OrganizationsProvider } from '../lib/organizations'

export const adminUser = {
  id: '01a1cb34-659b-74a8-983d-085c0383e0a6',
  email: 'admin@example.com',
  instance_admin: true,
  disabled: false,
  created_at: '2026-11-11T15:22:51.1Z',
}

export const customerUser = {
  id: '01a1cb34-659b-74a8-983d-085c0383e0b7',
  email: 'dev@acme.test',
  instance_admin: false,
  disabled: false,
  created_at: '2026-11-11T15:22:51.1Z',
}

export const acme = {
  id: '01a1cb34-29a5-74c3-9691-5394d021b0f9',
  slug: 'acme',
  name: 'Acme',
  status: 'active',
  status_reason: '',
  regions: [{ slug: 'eu-west', name: 'EU West', plan: { slug: 'free', name: 'Free' } }],
  members: 1,
  role: 'owner',
  created_at: '2026-11-11T15:22:35.8Z',
}

export const acmeMembership: MeOrganization = {
  id: acme.id,
  slug: 'acme',
  name: 'Acme',
  role: 'owner',
  status: 'active',
}

export function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

export interface Call {
  method: string
  url: string
  org: string | null
  body: unknown
}

// Stubs fetch with routes keyed "METHOD /path?query", or "METHOD /path" for
// any query. /v0/me answers with `user` and `organizations`, /v0/instance
// with a closed instance, unless a route says otherwise;
// anything else unknown fails the test.
export function stubAPI(
  routes: Record<string, (call: Call) => Response>,
  user = adminUser,
  organizations: MeOrganization[] = [],
): Call[] {
  const calls: Call[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString()
      const method = init?.method ?? 'GET'
      const headers = new Headers(init?.headers)
      const call: Call = {
        method,
        url,
        org: headers.get('Lewbox-Organization'),
        body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined,
      }
      calls.push(call)
      const handler = routes[`${method} ${url}`] ?? routes[`${method} ${url.split('?')[0]}`]
      if (!handler && url === '/v0/me') return json(200, { user, organizations, via: 'session' })
      if (!handler && url === '/v0/instance') return json(200, { name: 'Lewbox', signup: 'closed' })
      if (!handler) throw new Error(`unexpected request: ${method} ${url}`)
      return handler(call)
    }),
  )
  return calls
}

// Renders routes at `path` inside the console's providers.
export function renderAt(path: string, routes: ReactNode) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <AppLinkProvider>
        <InstanceProvider>
          <AuthProvider>
            <OrganizationsProvider>
              <Routes>{routes}</Routes>
            </OrganizationsProvider>
          </AuthProvider>
        </InstanceProvider>
      </AppLinkProvider>
    </MemoryRouter>,
  )
}

// Renders the whole app at `path`, with every route.
export function renderApp(path: string) {
  return renderAt(path, <Route path="*" element={<App />} />)
}
