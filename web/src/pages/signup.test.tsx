import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { customerUser, json, renderApp, stubAPI } from '../test/render'

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

  it('creates only the account and goes on to onboarding', async () => {
    let signedUp = false
    const calls = stubAPI({
      'GET /v0/me': () =>
        signedUp
          ? json(200, { user: customerUser, organizations: [], via: 'session' })
          : signedOut(),
      'GET /v0/instance': instance('open'),
      'POST /v0/auth/signup': () => {
        signedUp = true
        return json(201, { expires_at: '2026-12-11T15:22:51Z', user: customerUser })
      },
    })
    renderApp('/signup')

    fireEvent.change(await screen.findByLabelText('Email'), { target: { value: 'dev@acme.test' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'correct horse battery' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }))

    expect(await screen.findByRole('heading', { name: 'New workspace' })).toBeInTheDocument()
    expect(screen.queryByLabelText('Workspace')).toBeNull()
    const signup = calls.find((call) => call.method === 'POST' && call.url === '/v0/auth/signup')
    expect(signup?.body).toEqual({ email: 'dev@acme.test', password: 'correct horse battery' })
  })

  it('says so when sign-up is closed', async () => {
    stubAPI({ 'GET /v0/me': signedOut, 'GET /v0/instance': instance('closed') })
    renderApp('/signup')
    expect(await screen.findByRole('heading', { name: 'Sign-up is closed' })).toBeInTheDocument()
    expect(screen.queryByLabelText('Email')).toBeNull()
  })
})
