import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { acme, acmeMembership, customerUser, json, renderApp, stubAPI, type Call } from '../../test/render'

const token = 'lba_' + 'a'.repeat(58) + 'AbCdEf'

const member = {
  id: '01a1cb34-29a5-74c3-9691-5394d021b0c1',
  organization_id: acme.id,
  user_id: customerUser.id,
  email: customerUser.email,
  role: 'owner',
  created_at: '2026-11-11T15:22:35.8Z',
}

function stubTeam(routes: Record<string, (call: Call) => Response> = {}) {
  return stubAPI(
    {
      'GET /v0/organizations/acme': () => json(200, acme),
      'GET /v0/members': () => json(200, { data: [member] }),
      'GET /v0/admin-keys': () => json(200, { data: [] }),
      'POST /v0/admin-keys': (call) =>
        json(201, {
          id: '01a1cb34-29a5-74c3-9691-5394d021b0fa',
          organization_id: acme.id,
          name: (call.body as { name: string }).name,
          prefix: token.slice(0, 12),
          last4: token.slice(-4),
          status: 'active',
          created_at: '2026-11-11T15:30:00Z',
          token,
        }),
      ...routes,
    },
    customerUser,
    [acmeMembership],
  )
}

async function openKeys() {
  const tabs = await screen.findByRole('navigation', { name: 'Team views' })
  fireEvent.click(within(tabs).getByRole('link', { name: 'Management API keys' }))
  expect(await screen.findByText('No management API keys')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Create management key' }))
  expect(await screen.findByRole('heading', { name: 'Create management key' })).toBeInTheDocument()
  fireEvent.change(await screen.findByLabelText('Name'), { target: { value: 'backend' } })
}

describe('TeamPage', () => {
  it('lists members, and creates a management key shown once', async () => {
    const calls = stubTeam()
    renderApp('/team')
    expect(await screen.findByText(customerUser.email)).toBeInTheDocument()

    await openKeys()
    fireEvent.click(screen.getByRole('button', { name: 'Create key' }))

    expect(await screen.findByText(token)).toBeInTheDocument()
    expect(screen.getByText('Copy it now. You won’t see it again.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copy management key' })).toBeInTheDocument()
    const create = calls.find((call) => call.method === 'POST' && call.url === '/v0/admin-keys')
    expect(create?.org).toBe('acme')
    expect(create?.body).toEqual({ name: 'backend', expires_in: '90d' })
  })

  it('sends an empty expiry for a management key that never expires', async () => {
    const calls = stubTeam()
    renderApp('/team?tab=keys')

    await openKeys()
    fireEvent.click(screen.getByRole('combobox', { name: 'Expires' }))
    const never = await screen.findByRole('option', { name: 'Never' })
    fireEvent.pointerDown(never)
    fireEvent.click(never)
    await waitFor(() => expect(screen.getByRole('combobox', { name: 'Expires' })).toHaveTextContent('Never'))
    fireEvent.click(screen.getByRole('button', { name: 'Create key' }))

    expect(await screen.findByText(token)).toBeInTheDocument()
    expect(calls.find((call) => call.method === 'POST')?.body).toEqual({ name: 'backend', expires_in: '' })
  })

  it('renames the workspace at the top of the page', async () => {
    const calls = stubTeam({
      'PATCH /v0/organizations/acme': () => json(200, { ...acme, name: 'Acme Labs' }),
    })
    renderApp('/team')

    const name = await screen.findByLabelText('Workspace name')
    expect(name).toHaveValue('Acme')
    expect(screen.getByRole('button', { name: 'Rename' })).toBeDisabled()
    fireEvent.change(name, { target: { value: ' Acme Labs ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Rename' }))

    await waitFor(() =>
      expect(calls.find((call) => call.method === 'PATCH')).toMatchObject({
        url: '/v0/organizations/acme',
        body: { name: 'Acme Labs' },
      }),
    )
    // Leaving is only in the member's own menu now.
    expect(screen.queryByRole('button', { name: /Leave/ })).toBeNull()
    fireEvent.click(await screen.findByRole('button', { name: `Actions for ${customerUser.email}` }))
    expect(await screen.findByRole('menuitem', { name: 'Leave workspace' })).toBeInTheDocument()
  })

  it('doesn’t offer a rename to a member who can’t manage the workspace', async () => {
    stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, { ...acme, role: 'member' }),
        'GET /v0/members': () => json(200, { data: [{ ...member, role: 'member' }] }),
      },
      customerUser,
      [{ ...acmeMembership, role: 'member' }],
    )
    renderApp('/team')

    expect(await screen.findByText(customerUser.email)).toBeInTheDocument()
    expect(screen.queryByLabelText('Workspace name')).toBeNull()
  })
})
