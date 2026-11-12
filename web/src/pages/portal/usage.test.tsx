import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { daysAgo } from '../../lib/format'
import { acme, acmeMembership, customerUser, json, renderApp, stubAPI } from '../../test/render'

const euWest = { slug: 'eu-west', name: 'EU West', endpoint_url: 'https://eu.example', runtime: 'docker' }
const usEast = { slug: 'us-east', name: 'US East', endpoint_url: 'https://us.example', runtime: 'docker' }

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
      regions: { 'eu-west': { vcpu_hours: 2 }, 'us-east': { vcpu_hours: 1 } },
    },
  ],
}

describe('UsagePage', () => {
  it('names each region in the chart and leaves limits to Home', async () => {
    const calls = stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/regions': () => json(200, { data: [euWest, usEast] }),
        'GET /v0/usage': () => json(200, usage),
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/usage')

    expect(await screen.findByText('vCPU-hours')).toBeInTheDocument()
    expect((await screen.findAllByText('EU West')).length).toBeGreaterThan(0)
    expect(screen.getAllByText('US East').length).toBeGreaterThan(0)
    expect(screen.queryByText('eu-west')).toBeNull()
    expect(screen.queryByRole('heading', { name: /Limits/ })).toBeNull()
    expect(calls.some((call) => call.url.startsWith('/v0/limits'))).toBe(false)
    expect(calls.find((call) => call.url.startsWith('/v0/usage'))?.url).toContain(`from=${encodeURIComponent(daysAgo(30))}`)
  })
})
