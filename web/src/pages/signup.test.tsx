import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { acme, acmeMembership, customerUser, json, renderApp, stubAPI } from '../test/render'

const signedOut = () => json(401, { error: { code: 'unauthorized', message: 'session expired or invalid' } })

function instance(signup: string) {
  return () => json(200, { name: 'Acme Cloud', signup })
}

describe('sign-up', () => {
  it('hides the sign-up link when sign-up is closed', async () => {
    stubAPI({ 'GET /v0/me': signedOut, 'GET /v0/instance': instance('closed') })
    renderApp('/login')
    expect(await screen.findByRole('heading', { name: 'Sign in to Acme Cloud' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Create an account' })).toBeNull()
  })

  it('links to sign-up when it is open', async () => {
    stubAPI({ 'GET /v0/me': signedOut, 'GET /v0/instance': instance('approval') })
    renderApp('/login')
    expect(await screen.findByRole('link', { name: 'Create an account' })).toHaveAttribute('href', '/signup')
  })

  it('creates the account and opens the portal', async () => {
    let signedUp = false
    const calls = stubAPI({
      'GET /v0/me': () =>
        signedUp ? json(200, { user: customerUser, organizations: [acmeMembership], via: 'session' }) : signedOut(),
      'GET /v0/instance': instance('open'),
      'POST /v0/auth/signup': () => {
        signedUp = true
        return json(201, { expires_at: '2026-12-11T15:22:51Z', user: customerUser })
      },
      'GET /v0/organizations/acme': () => json(200, acme),
      'GET /v0/usage': () =>
        json(200, { from: '', to: '', granularity: 'day', totals: { sandbox_hours: 0, vcpu_hours: 0, memory_gib_hours: 0 }, data: [] }),
      'GET /v0/sandboxes': () => json(200, { data: [] }),
      'GET /v0/sandbox-keys': () => json(200, { data: [] }),
      'GET /v0/regions': () => json(200, { data: [] }),
    })
    renderApp('/signup')

    fireEvent.change(await screen.findByLabelText('Email'), { target: { value: 'dev@acme.test' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'correct horse battery' } })
    fireEvent.change(screen.getByLabelText('Workspace'), { target: { value: 'Acme' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }))

    expect(await screen.findByRole('heading', { name: 'Get started' })).toBeInTheDocument()
    const signup = calls.find((call) => call.method === 'POST' && call.url === '/v0/auth/signup')
    expect(signup?.body).toEqual({ email: 'dev@acme.test', password: 'correct horse battery', organization: 'Acme' })
  })

  it('says so when sign-up is closed', async () => {
    stubAPI({ 'GET /v0/me': signedOut, 'GET /v0/instance': instance('closed') })
    renderApp('/signup')
    expect(await screen.findByRole('heading', { name: 'Sign-up is closed' })).toBeInTheDocument()
    expect(screen.queryByLabelText('Email')).toBeNull()
  })
})
