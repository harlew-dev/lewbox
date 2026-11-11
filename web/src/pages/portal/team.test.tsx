import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { acme, acmeMembership, customerUser, json, renderApp, stubAPI } from '../../test/render'

const token = 'lba_' + 'a'.repeat(58) + 'AbCdEf'

describe('TeamPage', () => {
  it('lists members, and creates a management API key shown once', async () => {
    const calls = stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/members': () =>
          json(200, {
            data: [
              {
                id: '01a1cb34-29a5-74c3-9691-5394d021b0c1',
                organization_id: acme.id,
                user_id: customerUser.id,
                email: customerUser.email,
                role: 'owner',
                created_at: '2026-11-11T15:22:35.8Z',
              },
            ],
          }),
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
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/team')
    expect(await screen.findByText(customerUser.email)).toBeInTheDocument()

    const tabs = await screen.findByRole('navigation', { name: 'Team views' })
    fireEvent.click(within(tabs).getByRole('link', { name: 'Management API keys' }))
    expect(await screen.findByText('No management API keys')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Create API key' }))
    fireEvent.change(await screen.findByLabelText('Name'), { target: { value: 'backend' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create key' }))

    expect(await screen.findByText(token)).toBeInTheDocument()
    expect(screen.getByText(/can't show it again/)).toBeInTheDocument()
    const create = calls.find((call) => call.method === 'POST' && call.url === '/v0/admin-keys')
    expect(create?.org).toBe('acme')
    expect(create?.body).toEqual({ name: 'backend', expires_in: '90d' })
  })
})
