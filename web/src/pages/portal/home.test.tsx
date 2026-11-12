import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { daysAgo } from '../../lib/format'
import { acme, acmeMembership, customerUser, json, renderApp, stubAPI } from '../../test/render'

const bucket = (start: string, vcpu_hours: number) => ({
  start,
  sandbox_hours: vcpu_hours / 2,
  vcpu_hours,
  memory_gib_hours: vcpu_hours * 2,
  peak_sandboxes: 1,
  regions: { 'eu-west': { vcpu_hours } },
})

// 60 days ago falls outside the tiles' 30 days but inside the 90 Home reads.
const usage = {
  from: daysAgo(90),
  to: new Date().toISOString(),
  granularity: 'day',
  totals: { sandbox_hours: 5.25, vcpu_hours: 10.5, memory_gib_hours: 21 },
  data: [bucket(daysAgo(60), 4), bucket(daysAgo(2), 6.5)],
}

const noUsage = { ...usage, totals: { sandbox_hours: 0, vcpu_hours: 0, memory_gib_hours: 0 }, data: [] }

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

const limits = {
  regions: [
    {
      slug: 'eu-west',
      name: 'EU West',
      plan: { slug: 'free', name: 'Free' },
      limits: { sandboxes: 2, vcpus: null, memory_mb: 4096 },
      used: { sandboxes: 1, vcpus: 1, memory_mb: 1024 },
      error: '',
    },
  ],
}

describe('HomePage', () => {
  it('says the provider will enable a region when the workspace has none', async () => {
    const calls = stubAPI(
      { 'GET /v0/organizations/acme': () => json(200, { ...acme, region: null }) },
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

  it('shows 30 days of usage and each region’s limits once a sandbox has run', async () => {
    const calls = stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/usage': () => json(200, usage),
        'GET /v0/sandboxes': () => json(200, { data: [sandbox] }),
        'GET /v0/limits': () => json(200, limits),
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/')

    expect(await screen.findByText('Free plan')).toBeInTheDocument()
    expect(screen.getByText('in EU West')).toBeInTheDocument()
    expect(screen.getByText('1 of 2')).toBeInTheDocument()
    expect(screen.getByText('1 · Unlimited')).toBeInTheDocument()
    expect(screen.getByRole('meter', { name: 'vCPUs' })).not.toHaveAttribute('aria-valuemax')
    expect(screen.getByText('1 GB of 4 GB')).toBeInTheDocument()

    // The tiles count the last 30 days only, the range Usage opens with.
    expect(screen.getByText('vCPU-hours (30 days)')).toBeInTheDocument()
    expect(screen.getByText('6.5')).toBeInTheDocument()
    expect(screen.getByText('13 GiB-hours of memory')).toBeInTheDocument()
    expect(screen.getByText('Sandbox-hours (30 days)')).toBeInTheDocument()
    expect(screen.getByText('3.3')).toBeInTheDocument()
    expect(screen.queryByText(/credits/i)).toBeNull()
    expect(calls.find((call) => call.url.startsWith('/v0/usage'))?.url).toContain(
      `from=${encodeURIComponent(daysAgo(90))}`,
    )
  })

  it('opens the dashboard when sandboxes ran in the last 90 days but none exist now', async () => {
    stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/usage': () => json(200, { ...usage, data: [bucket(daysAgo(60), 4)] }),
        'GET /v0/sandboxes': () => json(200, { data: [] }),
        'GET /v0/limits': () => json(200, limits),
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/')

    expect(await screen.findByText('Free plan')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Get started' })).toBeNull()
  })

  it('says so when usage fails to load, rather than guessing', async () => {
    stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/usage': () => json(500, { error: { code: 'internal', message: 'usage is down' } }),
        'GET /v0/sandboxes': () => json(200, { data: [] }),
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/')

    expect(await screen.findByText('usage is down')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Get started' })).toBeNull()
  })

  it('tells a viewer of an active workspace why they can’t create a key', async () => {
    stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, { ...acme, role: 'viewer' }),
        'GET /v0/usage': () => json(200, noUsage),
        'GET /v0/sandboxes': () => json(200, { data: [] }),
        'GET /v0/sandbox-keys': () => json(200, { data: [] }),
        'GET /v0/regions': () => json(200, { data: [] }),
      },
      customerUser,
      [{ ...acmeMembership, role: 'viewer' }],
    )
    renderApp('/')

    expect(await screen.findByRole('button', { name: /Create API key/ })).toBeDisabled()
    expect(screen.getByText('Viewers can’t create API keys')).toBeInTheDocument()
    expect(screen.queryByText(/isn’t active yet/)).toBeNull()
  })

  it('says a pending workspace isn’t active yet', async () => {
    stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, { ...acme, status: 'pending' }),
        'GET /v0/usage': () => json(200, noUsage),
        'GET /v0/sandboxes': () => json(200, { data: [] }),
        'GET /v0/sandbox-keys': () => json(200, { data: [] }),
        'GET /v0/regions': () => json(200, { data: [] }),
      },
      customerUser,
      [{ ...acmeMembership, status: 'pending' }],
    )
    renderApp('/')

    expect(await screen.findByRole('button', { name: /Create API key/ })).toBeDisabled()
    expect(screen.getByText('Your workspace isn’t active yet')).toBeInTheDocument()
  })
})
