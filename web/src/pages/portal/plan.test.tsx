import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { acme, acmeMembership, customerUser, json, renderApp, stubAPI } from '../../test/render'

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

describe('PlanPage', () => {
  it('names the plan and region, with what is used against each limit', async () => {
    stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/limits': () => json(200, limits),
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/plan')

    expect(await screen.findByRole('heading', { name: 'Plan & Billing' })).toBeInTheDocument()
    expect((await screen.findAllByText('Free')).length).toBeGreaterThan(0)
    expect((await screen.findAllByText('in EU West')).length).toBeGreaterThan(0)
    expect(await screen.findByText('1 of 2')).toBeInTheDocument()
    expect(screen.getByText('1 · Unlimited')).toBeInTheDocument()
    expect(screen.getByText('1 GB of 4 GB')).toBeInTheDocument()
    expect(screen.getByText('To change your plan, contact Lewbox.')).toBeInTheDocument()
    expect(screen.queryByText(/price|credit|currency/i)).toBeNull()
  })

  it('says the provider will assign a region when there is none', async () => {
    stubAPI(
      { 'GET /v0/organizations/acme': () => json(200, { ...acme, region: null }) },
      customerUser,
      [acmeMembership],
    )
    renderApp('/plan')

    expect(
      await screen.findByText('Your workspace doesn’t have a region yet. Your provider will enable one.'),
    ).toBeInTheDocument()
  })
})
