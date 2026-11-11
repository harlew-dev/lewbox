import { render } from '@testing-library/react'
import type { ReactNode } from 'react'
import { MemoryRouter, Routes } from 'react-router'
import { vi } from 'vitest'
import { AppLinkProvider } from '../components/app-link'
import { AuthProvider } from '../lib/auth'
import { OrganizationsProvider } from '../lib/organizations'

export const adminUser = {
  id: '01a1cb34-659b-74a8-983d-085c0383e0a6',
  email: 'admin@example.com',
  instance_admin: true,
  disabled: false,
  created_at: '2026-11-11T15:22:51.1Z',
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

// Stubs fetch with routes keyed "METHOD /path" (the path may include a
// query). /v0/me answers with `user`; anything unknown fails the test.
export function stubAPI(routes: Record<string, (call: Call) => Response>, user = adminUser): Call[] {
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
      if (url === '/v0/me') return json(200, { user, via: 'session' })
      const handler = routes[`${method} ${url}`]
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
        <AuthProvider>
          <OrganizationsProvider>
            <Routes>{routes}</Routes>
          </OrganizationsProvider>
        </AuthProvider>
      </AppLinkProvider>
    </MemoryRouter>,
  )
}
