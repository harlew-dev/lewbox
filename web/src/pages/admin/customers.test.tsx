import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { acme, json, renderApp, stubAPI } from '../../test/render'

const pending = { ...acme, status: 'pending', role: undefined, overrides: { sandboxes: null, vcpus: null, memory_mb: null } }

const limits = {
  limits: acme.limits,
  used: { sandboxes: 0, vcpus: 0, memory_mb: 0 },
  regions: [],
}

const usage = {
  from: '2026-10-13T00:00:00Z',
  to: '2026-11-11T16:00:00Z',
  granularity: 'day',
  totals: { sandbox_hours: 0, vcpu_hours: 0, memory_gib_hours: 0 },
  data: [],
}

describe('customers', () => {
  it('lists customers with a tab for those waiting', async () => {
    stubAPI({
      'GET /v0/organizations': () => json(200, { data: [pending] }),
      'GET /v0/sandboxes?status=started': () => json(200, { data: [] }),
    })
    renderApp('/admin/customers?tab=pending')
    const row = (await screen.findAllByRole('link', { name: /Acme/ }))[0]!
    expect(row).toHaveAttribute('href', '/admin/customers/acme')
    const tabs = screen.getByRole('navigation', { name: 'Customer views' })
    expect(within(tabs).getByRole('link', { name: 'Pending' })).toHaveAttribute('aria-current', 'page')
  })

  it('approves a pending customer', async () => {
    let status = 'pending'
    const calls = stubAPI({
      'GET /v0/organizations?status=pending': () => json(200, { data: status === 'pending' ? [pending] : [] }),
      'GET /v0/organizations/acme': () => json(200, { ...pending, status }),
      'PATCH /v0/organizations/acme': () => {
        status = 'active'
        return json(200, { ...pending, status })
      },
      'GET /v0/limits': () => json(200, limits),
      'GET /v0/usage': () => json(200, usage),
    })
    renderApp('/admin/customers/acme')

    fireEvent.click(await screen.findByRole('button', { name: 'Approve' }))
    await waitFor(() =>
      expect(calls.find((call) => call.method === 'PATCH')).toMatchObject({
        url: '/v0/organizations/acme',
        body: { status: 'active' },
      }),
    )
    expect(await screen.findByRole('button', { name: 'Suspend' })).toBeInTheDocument()
  })
})
