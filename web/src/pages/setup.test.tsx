import { fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import { customerUser, json, renderApp, stubAPI } from '../test/render'

const signedOut = () => json(401, { error: { code: 'unauthorized', message: 'session expired or invalid' } })

const info = {
  instance_name: 'Acme Cloud',
  plan: { name: 'Pro' },
  region: { name: 'EU West' },
  expires_at: '2026-11-19T15:22:51Z',
}

beforeEach(() => {
  sessionStorage.clear()
})

describe('setup link page', () => {
  it('says when a link is invalid or expired', async () => {
    stubAPI({
      'GET /v0/me': signedOut,
      'GET /v0/instance': () => json(200, { name: 'Acme Cloud', signup: 'closed' }),
      'GET /v0/setup/lbs_bad': () =>
        json(404, { error: { code: 'not_found', message: 'this setup link is invalid or has expired' } }),
    })
    renderApp('/setup/lbs_bad')

    expect(
      await screen.findByText('This setup link is invalid or has expired. Ask Acme Cloud for a new one.'),
    ).toBeInTheDocument()
    expect(screen.queryByLabelText('Email')).toBeNull()
  })

  it('signs up with the token and continues to onboarding', async () => {
    let signedUp = false
    const calls = stubAPI({
      'GET /v0/me': () =>
        signedUp
          ? json(200, { user: customerUser, organizations: [], via: 'session' })
          : signedOut(),
      'GET /v0/instance': () => json(200, { name: 'Acme Cloud', signup: 'closed' }),
      'GET /v0/setup/lbs_test': () => json(200, info),
      'POST /v0/auth/signup': () => {
        signedUp = true
        return json(201, { expires_at: '2026-12-11T15:22:51Z', user: customerUser })
      },
    })
    renderApp('/setup/lbs_test')

    expect(await screen.findByText('Join Acme Cloud on the Pro plan in EU West.')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'dev@acme.test' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'correct horse battery' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }))

    expect(await screen.findByRole('heading', { name: 'New workspace' })).toBeInTheDocument()
    await waitFor(() =>
      expect(calls.find((call) => call.method === 'POST')?.body).toEqual({
        email: 'dev@acme.test',
        password: 'correct horse battery',
        setup_token: 'lbs_test',
      }),
    )
  })

  it('lets a signed-in visitor continue with the token', async () => {
    stubAPI(
      {
        'GET /v0/setup/lbs_test': () => json(200, info),
      },
      customerUser,
      [],
    )
    renderApp('/setup/lbs_test')

    fireEvent.click(await screen.findByRole('button', { name: 'Continue as dev@acme.test' }))
    expect(await screen.findByRole('heading', { name: 'New workspace' })).toBeInTheDocument()
    expect(screen.getByLabelText('Name')).toBeInTheDocument()
  })

  it('signs a signed-in visitor out in place so they can register', async () => {
    const calls = stubAPI(
      {
        'GET /v0/setup/lbs_test': () => json(200, info),
        'POST /v0/auth/logout': () => json(200, { ok: true }),
      },
      customerUser,
      [],
    )
    renderApp('/setup/lbs_test')

    fireEvent.click(await screen.findByRole('button', { name: 'Sign out' }))

    expect(await screen.findByLabelText('Email')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Set up your account' })).toBeInTheDocument()
    expect(calls.some((call) => call.method === 'POST' && call.url === '/v0/auth/logout')).toBe(true)
  })
})
