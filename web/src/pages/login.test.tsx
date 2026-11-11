import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AppLinkProvider } from '../components/app-link'
import { AuthProvider } from '../lib/auth'
import { LoginPage } from './login'

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url === '/v0/me') {
        return json(401, {
          error: { code: 'unauthorized', message: 'session expired or invalid' },
        })
      }
      if (url === '/v0/auth/session') {
        return json(401, {
          error: { code: 'unauthorized', message: 'email or password is wrong' },
        })
      }
      throw new Error(`unexpected request: ${url}`)
    }),
  )
})

describe('LoginPage', () => {
  it('shows the server error when the credentials are wrong', async () => {
    render(
      <MemoryRouter initialEntries={['/login']}>
        <AppLinkProvider>
          <AuthProvider>
            <Routes>
              <Route path="/login" element={<LoginPage />} />
            </Routes>
          </AuthProvider>
        </AppLinkProvider>
      </MemoryRouter>,
    )

    expect(
      await screen.findByRole('heading', { name: 'Sign in to Lewbox' }),
    ).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Email'), {
      target: { value: 'admin@example.com' },
    })
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'wrong-password' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(
      await screen.findByText('Email or password is wrong.'),
    ).toBeInTheDocument()
  })
})
