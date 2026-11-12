import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { acme, acmeMembership, customerUser, json, renderApp, stubAPI } from '../../test/render'

const usage = {
  from: '2026-11-01T00:00:00Z',
  to: '2026-11-12T00:00:00Z',
  granularity: 'hour',
  totals: { sandbox_hours: 3, vcpu_hours: 6.5, memory_gib_hours: 12 },
  data: [],
}

const sandbox = {
  id: 'sb-1',
  region: 'eu-west',
  name: '',
  image: 'ubuntu:22.04',
  status: 'started',
  status_reason: '',
  runtime: 'docker',
  cpu: 1,
  memory_mb: 1024,
  disk_gb: 10,
  created_at: '2026-11-12T10:00:00Z',
  last_seen_at: '2026-11-12T10:00:00Z',
}

describe('HomePage', () => {
  it('says the provider will enable a region when the workspace has none', async () => {
    const calls = stubAPI(
      { 'GET /v0/organizations/acme': () => json(200, { ...acme, regions: [] }) },
      customerUser,
      [acmeMembership],
    )
    renderApp('/')

    expect(
      await screen.findByText('Your workspace doesn’t have a region yet. Your provider will enable one.'),
    ).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Get started' })).toBeNull()
    expect(calls.some((call) => call.url.startsWith('/v0/sandbox-keys'))).toBe(false)
  })

  it('shows usage and each region’s limits once a sandbox has run', async () => {
    stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/usage': () => json(200, usage),
        'GET /v0/sandboxes': () => json(200, { data: [sandbox] }),
        'GET /v0/limits': () =>
          json(200, {
            regions: [
              {
                slug: 'eu-west',
                name: 'EU West',
                plan: { slug: 'free', name: 'Free' },
                limits: { sandboxes: 2, vcpus: null, memory_mb: 4096 },
                used: { sandboxes: 1, vcpus: 1, memory_mb: 1024 },
                quota: null,
                blocked: false,
                pushed_at: null,
                error: '',
              },
            ],
          }),
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/')

    const region = await screen.findByRole('region', { name: 'EU West limits' })
    expect(within(region).getByText('Free plan')).toBeInTheDocument()
    expect(within(region).getByText('1 of 2')).toBeInTheDocument()
    expect(within(region).getByText('1 · no limit')).toBeInTheDocument()
    expect(within(region).getByText('1 GB of 4 GB')).toBeInTheDocument()
    expect(screen.getByText('6.5')).toBeInTheDocument()
    expect(screen.queryByText(/credits/i)).toBeNull()
  })
})
