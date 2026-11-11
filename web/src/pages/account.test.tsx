import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AppLinkProvider } from '../components/app-link'
import { AuthProvider } from '../lib/auth'
import { AccountPage } from './account'

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
      if (url === '/v0/me') {
        return new Response(JSON.stringify({ user, via: 'session' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      throw new Error(`unexpected request: ${url}`)
    }),
  )
})

describe('AccountPage', () => {
  it('shows the account and the password form, and leaves signing out to the account menu', async () => {
    render(
      <MemoryRouter initialEntries={['/account']}>
        <AppLinkProvider>
          <AuthProvider>
            <AccountPage />
          </AuthProvider>
        </AppLinkProvider>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('heading', { name: 'Account settings' })).toBeInTheDocument()
    expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Change password' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /sign out/i })).not.toBeInTheDocument()
    expect(screen.queryByText(/sign out/i, { selector: 'h2,h3' })).not.toBeInTheDocument()
  })
})
