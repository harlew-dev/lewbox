import { fireEvent, screen } from '@testing-library/react'
import { Route } from 'react-router'
import { describe, expect, it } from 'vitest'
import { json, renderAt, stubAPI } from '../../test/render'
import { ApiKeysPage } from './api-keys'
import { OrganizationRoute } from './organization-route'

const org = {
  id: '01a1cb34-29a5-74c3-9691-5394d021b0f9',
  slug: 'acme',
  name: 'Acme',
  status: 'active',
  members: 2,
  role: 'owner',
  created_at: '2026-11-11T15:22:35.8Z',
}

const token = 'lba_' + 'a'.repeat(58) + 'AbCdEf'

describe('ApiKeysPage', () => {
  it('creates a key for the organization and shows its token once', async () => {
    const calls = stubAPI({
      'GET /v0/organizations': () => json(200, { data: [org] }),
      'GET /v0/organizations/acme': () => json(200, org),
      'GET /v0/admin-keys': () => json(200, { data: [] }),
      'POST /v0/admin-keys': (call) =>
        json(201, {
          id: '01a1cb34-29a5-74c3-9691-5394d021b0fa',
          organization_id: org.id,
          name: (call.body as { name: string }).name,
          prefix: token.slice(0, 12),
          last4: token.slice(-4),
          status: 'active',
          created_at: '2026-11-11T15:30:00Z',
          token,
        }),
    })
    renderAt(
      '/organizations/acme/api-keys',
      <Route path="/organizations/:org" element={<OrganizationRoute />}>
        <Route path="api-keys" element={<ApiKeysPage />} />
      </Route>,
    )

    expect(await screen.findByText('No API keys')).toBeInTheDocument()
    fireEvent.click(screen.getAllByRole('button', { name: 'Create API key' })[0]!)
    fireEvent.change(await screen.findByLabelText('Name'), { target: { value: 'backend' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create key' }))

    expect(await screen.findByText(token)).toBeInTheDocument()
    expect(screen.getByText(/can't show it again/)).toBeInTheDocument()
    const create = calls.find((call) => call.method === 'POST' && call.url === '/v0/admin-keys')
    expect(create?.org).toBe('acme')
    expect(create?.body).toEqual({ name: 'backend', expires_in: '90d' })
  })
})
