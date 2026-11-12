import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { json, renderApp, stubAPI } from '../../test/render'

const settings = {
  instance_name: 'Lewbox',
  signup: 'closed',
  default_plan: 'free',
  default_region: 'eu-west',
}

const plans = [
  { id: 'p1', slug: 'free', name: 'Free', max_sandboxes: 2, max_vcpus: 2, max_memory_mb: 4096, subscriptions: 1, default: true, created_at: '' },
  { id: 'p2', slug: 'pro', name: 'Pro', max_sandboxes: 20, max_vcpus: null, max_memory_mb: null, subscriptions: 0, default: false, created_at: '' },
]

const regions = [
  { slug: 'eu-west', name: 'EU West', endpoint_url: 'https://eu.example', runtime: 'docker', status: 'ready' },
  { slug: 'us-east', name: 'US East', endpoint_url: 'https://us.example', runtime: 'docker', status: 'ready' },
]

function stub() {
  return stubAPI({
    'GET /v0/organizations': () => json(200, { data: [] }),
    'GET /v0/plans': () => json(200, { data: plans }),
    'GET /v0/regions': () => json(200, { data: regions }),
    'GET /v0/settings': () => json(200, settings),
    'PATCH /v0/settings': (call) => json(200, { ...settings, ...(call.body as object) }),
  })
}

describe('AdminSettingsPage', () => {
  it('saves only what changed', async () => {
    const calls = stub()
    renderApp('/admin/settings')

    const name = await screen.findByLabelText('Instance name')
    expect(screen.getByRole('button', { name: 'Save settings' })).toBeDisabled()
    expect(within(screen.getByRole('main')).queryByText(/billing|credits|currency/i)).toBeNull()
    fireEvent.change(name, { target: { value: 'Acme Cloud' } })
    fireEvent.click(screen.getByRole('radio', { name: /Approval/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))

    await waitFor(() =>
      expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({
        instance_name: 'Acme Cloud',
        signup: 'approval',
      }),
    )
  })

  it('sets the one default region for new workspaces', async () => {
    const calls = stub()
    renderApp('/admin/settings')

    const region = await screen.findByRole('combobox', { name: 'Default region' })
    expect(region).toHaveTextContent('EU West')
    fireEvent.click(region)
    const us = await screen.findByRole('option', { name: 'US East' })
    fireEvent.pointerDown(us)
    fireEvent.click(us)
    await waitFor(() => expect(screen.getByRole('combobox', { name: 'Default region' })).toHaveTextContent('US East'))
    fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))

    await waitFor(() =>
      expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ default_region: 'us-east' }),
    )
  })
})
