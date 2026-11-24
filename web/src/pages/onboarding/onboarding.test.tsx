import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { acme, acmeMembership, customerUser, json, renderApp, stubAPI } from '../../test/render'

describe('onboarding', () => {
  it('takes a customer with no workspace there from the portal', async () => {
    stubAPI(
      {
        'GET /v0/instance': () => json(200, { name: 'Lewbox', signup: 'open' }),
      },
      customerUser,
      [],
    )
    renderApp('/')
    expect(await screen.findByRole('heading', { name: 'New workspace' })).toBeInTheDocument()
    expect(screen.getByLabelText('Name')).toBeInTheDocument()
  })

  it('creates the workspace and opens the tour', async () => {
    let hasWorkspace = false
    const calls = stubAPI(
      {
        'GET /v0/profile': () =>
          json(200, {
            user: customerUser,
            organizations: hasWorkspace ? [acmeMembership] : [],
            via: 'session',
          }),
        'GET /v0/instance': () => json(200, { name: 'Lewbox', signup: 'open' }),
        'POST /v0/organizations': () => {
          hasWorkspace = true
          return json(201, acme)
        },
      },
      customerUser,
      [],
    )
    renderApp('/onboarding')

    fireEvent.change(await screen.findByLabelText('Name'), { target: { value: 'Acme' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create workspace' }))

    expect(await screen.findByRole('heading', { name: 'Getting started' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Getting started' })).toBeInTheDocument()
    expect(calls.find((call) => call.method === 'POST' && call.url === '/v0/organizations')?.body).toEqual({ name: 'Acme' })
  })

  it('sends a visitor to the provider when sign-up is closed and there is no link', async () => {
    stubAPI(
      {
        'GET /v0/instance': () => json(200, { name: 'Acme Cloud', signup: 'closed' }),
      },
      customerUser,
      [],
    )
    renderApp('/onboarding')

    expect(
      await screen.findByText('Ask Acme Cloud for a setup link to create a workspace.'),
    ).toBeInTheDocument()
    expect(screen.queryByLabelText('Name')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Account' }))
    expect(await screen.findByRole('menuitem', { name: 'Sign out' })).toBeInTheDocument()
  })

  it('shows the link’s plan and region and passes its token through', async () => {
    let hasWorkspace = false
    const calls = stubAPI(
      {
        'GET /v0/profile': () =>
          json(200, {
            user: customerUser,
            organizations: hasWorkspace ? [acmeMembership] : [],
            via: 'session',
          }),
        'POST /v0/setup-links/check': () =>
          json(200, {
            instance_name: 'Acme Cloud',
            plan: { name: 'Pro' },
            region: { name: 'EU West' },
            expires_at: '2026-11-19T15:22:51Z',
          }),
        'POST /v0/organizations': () => {
          hasWorkspace = true
          return json(201, acme)
        },
      },
      customerUser,
      [],
    )
    renderApp({ pathname: '/onboarding', state: { setupToken: 'lbs_test' } })

    expect(await screen.findByText('You’ll join on the Pro plan in EU West.')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Acme' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create workspace' }))

    expect(await screen.findByRole('heading', { name: 'Getting started' })).toBeInTheDocument()
    await waitFor(() =>
      expect(calls.find((call) => call.method === 'POST' && call.url === '/v0/organizations')?.body).toEqual({
        name: 'Acme',
        setup_token: 'lbs_test',
      }),
    )
  })

  it('forgets a stale link and points a closed instance to its provider', async () => {
    stubAPI(
      {
        'GET /v0/instance': () => json(200, { name: 'Acme Cloud', signup: 'closed' }),
        'POST /v0/setup-links/check': () =>
          json(404, { error: { code: 'not_found', message: 'this setup link is invalid or has expired' } }),
      },
      customerUser,
      [],
    )
    renderApp({ pathname: '/onboarding', state: { setupToken: 'lbs_stale' } })

    expect(await screen.findByText('This setup link is invalid or has expired.')).toBeInTheDocument()
    expect(
      await screen.findByText('Ask Acme Cloud for a setup link to create a workspace.'),
    ).toBeInTheDocument()
    expect(screen.queryByLabelText('Name')).toBeNull()
  })

  it('loses the token on a reload and sends no check', async () => {
    const calls = stubAPI(
      {
        'GET /v0/instance': () => json(200, { name: 'Acme Cloud', signup: 'closed' }),
      },
      customerUser,
      [],
    )
    renderApp('/onboarding')

    expect(
      await screen.findByText('Ask Acme Cloud for a setup link to create a workspace.'),
    ).toBeInTheDocument()
    expect(calls.some((call) => call.url === '/v0/setup-links/check')).toBe(false)
  })
})
