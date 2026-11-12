import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { daysAgo } from '../../lib/format'
import { acme, acmeMembership, customerUser, json, renderApp, stubAPI } from '../../test/render'

const usage = {
  from: daysAgo(30),
  to: new Date().toISOString(),
  granularity: 'day',
  totals: { sandbox_hours: 2, vcpu_hours: 3, memory_gib_hours: 4 },
  data: [
    {
      start: daysAgo(1),
      sandbox_hours: 2,
      vcpu_hours: 3,
      memory_gib_hours: 4,
      peak_sandboxes: 2,
      regions: { 'eu-west': { vcpu_hours: 3 } },
    },
  ],
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

describe('UsagePage', () => {
  it('shows usage, and the plan the workspace has in its one region', async () => {
    const calls = stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/usage': () => json(200, usage),
        'GET /v0/limits': () => json(200, limits),
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/usage')

    expect((await screen.findAllByText('vCPU-hours')).length).toBeGreaterThan(0)
    expect(await screen.findByText('Free plan')).toBeInTheDocument()
    expect(screen.getByText('in EU West')).toBeInTheDocument()
    expect(screen.getByText('1 of 2')).toBeInTheDocument()
    expect(screen.getByText('1 · Unlimited')).toBeInTheDocument()
    expect(screen.getByText('1 GB of 4 GB')).toBeInTheDocument()
    expect(screen.queryByText('eu-west')).toBeNull()
    expect(calls.find((call) => call.url.startsWith('/v0/usage'))?.url).toContain(`from=${encodeURIComponent(daysAgo(30))}`)
  })
})
