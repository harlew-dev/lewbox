import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { json, renderApp, stubAPI } from '../../test/render'

const free = {
  id: '01a1cb34-29a5-74c3-9691-5394d021b0a1',
  slug: 'free',
  name: 'Free',
  max_sandboxes: 2,
  max_vcpus: 2,
  max_memory_mb: 4096,
  subscriptions: 3,
  default: true,
  created_at: '2026-11-11T15:22:35.8Z',
}

describe('PlansPage', () => {
  it('creates a plan, leaving empty limits unlimited', async () => {
    const calls = stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/plans': () => json(200, { data: [free] }),
      'POST /v0/plans': (call) => json(201, { ...free, ...(call.body as object), id: 'x', default: false, subscriptions: 0 }),
    })
    renderApp('/admin/plans')

    expect(await screen.findByText('Default')).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: 'Workspaces' })).toBeInTheDocument()
    expect(screen.getByRole('cell', { name: '3' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'New plan' }))
    fireEvent.change(await screen.findByLabelText('Name'), { target: { value: 'Pro' } })
    fireEvent.change(screen.getByLabelText('Slug'), { target: { value: 'pro' } })
    fireEvent.change(screen.getByLabelText('Sandboxes'), { target: { value: '10' } })
    // A limit is never 0: empty is unlimited.
    fireEvent.change(screen.getByLabelText('vCPUs'), { target: { value: '0' } })
    expect(screen.getByRole('button', { name: 'Create plan' })).toBeDisabled()
    fireEvent.change(screen.getByLabelText('vCPUs'), { target: { value: '16' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create plan' }))

    await waitFor(() =>
      expect(calls.find((call) => call.method === 'POST')?.body).toEqual({
        name: 'Pro',
        slug: 'pro',
        max_sandboxes: 10,
        max_vcpus: 16,
        max_memory_mb: null,
      }),
    )
  })
})
