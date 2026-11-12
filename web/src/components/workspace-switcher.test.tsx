import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { acme, acmeMembership, customerUser, json, renderApp, stubAPI } from '../test/render'

const beta = { ...acmeMembership, id: '01a1cb34-29a5-74c3-9691-5394d021b0fa', slug: 'beta', name: 'Beta' }

const emptyUsage = {
  from: '',
  to: '',
  granularity: 'day' as const,
  totals: { sandbox_hours: 0, vcpu_hours: 0, memory_gib_hours: 0 },
  data: [],
}

function stubPortal(organizations = [acmeMembership, beta]) {
  return stubAPI(
    {
      'GET /v0/organizations/acme': () => json(200, acme),
      'GET /v0/usage': () => json(200, emptyUsage),
      'GET /v0/sandboxes': () => json(200, { data: [] }),
      'GET /v0/sandbox-keys': () => json(200, { data: [] }),
      'GET /v0/regions': () => json(200, { data: [] }),
      'GET /v0/me': () => json(200, { user: customerUser, organizations, via: 'session' }),
    },
    customerUser,
    organizations,
  )
}

async function openMenu() {
  const [button] = await screen.findAllByRole('button', { name: 'Workspace menu' })
  fireEvent.click(button!)
}

describe('WorkspaceSwitcher', () => {
  it('lists every workspace with the current one marked, and Workspace settings', async () => {
    stubPortal()
    renderApp('/')
    await openMenu()

    expect(await screen.findByRole('menuitem', { name: 'Workspace settings' })).toHaveAttribute('href', '/settings')
    const acmeItem = screen.getByRole('menuitem', { name: /Acme/ })
    expect(acmeItem).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('menuitem', { name: /Beta/ })).toBeInTheDocument()
  })

  it('hides New workspace when sign-up is closed and the user isn’t an instance admin', async () => {
    stubPortal()
    renderApp('/')
    await openMenu()

    expect(await screen.findByRole('menuitem', { name: 'Workspace settings' })).toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: /New workspace/ })).toBeNull()
  })

  it('creates a workspace and opens it', async () => {
    let organizations = [acmeMembership, beta]
    const gamma = { ...acmeMembership, id: '01a1cb34-29a5-74c3-9691-5394d021b0fb', slug: 'gamma', name: 'Gamma' }
    const calls = stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/organizations/gamma': () => json(200, { ...acme, id: gamma.id, slug: 'gamma', name: 'Gamma', region: null }),
        'GET /v0/usage': () => json(200, emptyUsage),
        'GET /v0/sandboxes': () => json(200, { data: [] }),
        'GET /v0/sandbox-keys': () => json(200, { data: [] }),
        'GET /v0/regions': () => json(200, { data: [] }),
        'GET /v0/instance': () => json(200, { name: 'Lewbox', signup: 'open' }),
        'GET /v0/me': () => json(200, { user: customerUser, organizations, via: 'session' }),
        'POST /v0/organizations': () => {
          organizations = [...organizations, gamma]
          return json(201, { ...acme, id: gamma.id, slug: 'gamma', name: 'Gamma', region: null })
        },
      },
      customerUser,
      organizations,
    )
    renderApp('/')
    await openMenu()
    fireEvent.click(await screen.findByRole('menuitem', { name: /New workspace/ }))

    fireEvent.change(await screen.findByLabelText('Workspace name'), { target: { value: 'Gamma' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create workspace' }))

    await waitFor(() =>
      expect(calls.find((call) => call.method === 'POST' && call.url === '/v0/organizations')?.body).toEqual({
        name: 'Gamma',
      }),
    )
    expect(await screen.findByText('Your workspace doesn’t have a region yet. Your provider will enable one.')).toBeInTheDocument()
    expect(calls.some((call) => call.method === 'GET' && call.url === '/v0/organizations/gamma')).toBe(true)
    const [button] = screen.getAllByRole('button', { name: 'Workspace menu' })
    expect(button).toHaveTextContent('Gamma')
  })
})
