import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { acme, json, renderApp, stubAPI, type Call } from '../../test/render'

const pending = { ...acme, status: 'pending', role: undefined, vcpu_hours_30d: 12.5 }

const usage = {
  from: '2026-10-13T00:00:00Z',
  to: '2026-11-11T16:00:00Z',
  granularity: 'day',
  totals: { sandbox_hours: 0, vcpu_hours: 0, memory_gib_hours: 0 },
  data: [],
}

const euWest = { slug: 'eu-west', name: 'EU West', endpoint_url: 'https://eu.example', runtime: 'docker', status: 'ready' }
const usEast = { slug: 'us-east', name: 'US East', endpoint_url: 'https://us.example', runtime: 'docker', status: 'ready' }

const free = { id: 'p1', slug: 'free', name: 'Free', max_sandboxes: 2, max_vcpus: 2, max_memory_mb: 4096, subscriptions: 1, default: true, created_at: '' }
const pro = { id: 'p2', slug: 'pro', name: 'Pro', max_sandboxes: 20, max_vcpus: 40, max_memory_mb: 81920, subscriptions: 0, default: false, created_at: '' }

const euSubscription = {
  region: euWest,
  plan: { slug: 'free', name: 'Free' },
  limits: { sandboxes: 2, vcpus: 2, memory_mb: 4096 },
  overrides: { sandboxes: null, vcpus: null, memory_mb: null },
  created_at: '2026-11-11T15:22:35.8Z',
}

const limits = {
  regions: [
    {
      slug: 'eu-west',
      name: 'EU West',
      plan: { slug: 'free', name: 'Free' },
      limits: euSubscription.limits,
      used: { sandboxes: 1, vcpus: 1, memory_mb: 1024 },
      quota: null,
      blocked: false,
      pushed_at: null,
      error: '',
    },
  ],
}

// What the customer page loads, for acme.
function detailRoutes(org: object) {
  return {
    'GET /v0/organizations/acme': () => json(200, org),
    'GET /v0/subscriptions': () => json(200, { data: [euSubscription] }),
    'GET /v0/limits': () => json(200, limits),
    'GET /v0/regions': () => json(200, { data: [euWest, usEast] }),
    'GET /v0/usage': () => json(200, usage),
    'GET /v0/plans': () => json(200, { data: [free, pro] }),
  }
}

describe('customers', () => {
  it('lists workspaces with their regions and a tab for those waiting', async () => {
    stubAPI({
      'GET /v0/organizations': () => json(200, { data: [pending] }),
      'GET /v0/sandboxes?status=started': () => json(200, { data: [] }),
    })
    renderApp('/admin/customers?tab=pending')
    const row = (await screen.findAllByRole('link', { name: /Acme/ }))[0]!
    expect(row).toHaveAttribute('href', '/admin/customers/acme')
    expect(screen.getByRole('columnheader', { name: 'Workspace' })).toBeInTheDocument()
    expect(screen.getByText('eu-west · Free')).toBeInTheDocument()
    expect(screen.getByText('12.5')).toBeInTheDocument()
    const tabs = screen.getByRole('navigation', { name: 'Customer views' })
    expect(within(tabs).getByRole('link', { name: 'Pending' })).toHaveAttribute('aria-current', 'page')
  })

  it('approves a pending customer', async () => {
    let status = 'pending'
    const calls = stubAPI({
      ...detailRoutes(pending),
      'GET /v0/organizations?status=pending': () => json(200, { data: status === 'pending' ? [pending] : [] }),
      'GET /v0/organizations/acme': () => json(200, { ...pending, status }),
      'PATCH /v0/organizations/acme': () => {
        status = 'active'
        return json(200, { ...pending, status })
      },
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

describe('new customer', () => {
  const settings = { instance_name: 'Lewbox', signup: 'closed', default_plan: 'pro', default_regions: ['eu-west'] }

  function stub(routes: Record<string, (call: Call) => Response>) {
    return stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/sandboxes?status=started': () => json(200, { data: [] }),
      'GET /v0/settings': () => json(200, settings),
      'GET /v0/regions': () => json(200, { data: [euWest, usEast] }),
      'GET /v0/plans': () => json(200, { data: [free, pro] }),
      'POST /v0/organizations': (call) => {
        const body = call.body as { slug: string; name: string }
        return json(201, { ...acme, slug: body.slug, name: body.name, role: undefined, regions: [] })
      },
      ...routes,
    })
  }

  async function fill() {
    fireEvent.click(await screen.findByRole('button', { name: 'New customer' }))
    fireEvent.change(await screen.findByLabelText('Owner email'), { target: { value: 'dev@acme.test' } })
    fireEvent.change(screen.getByLabelText('Workspace name'), { target: { value: 'Acme Corp' } })
    // The default plan in the default regions is filled in.
    expect(await screen.findByRole('combobox', { name: 'Region 1' })).toHaveTextContent('EU West')
    expect(screen.getByRole('combobox', { name: 'Plan in region 1' })).toHaveTextContent('Pro')
    expect(screen.queryByRole('combobox', { name: 'Region 2' })).toBeNull()
  }

  it('generates a password, then creates the account, the workspace and its region plan in order', async () => {
    const calls = stub({
      'POST /v0/users': (call) => json(201, { id: 'u1', email: (call.body as { email: string }).email, instance_admin: false, disabled: false, created_at: '' }),
      'PUT /v0/subscriptions/eu-west': () => json(200, { ...euSubscription, plan: { slug: 'pro', name: 'Pro' } }),
    })
    renderApp('/admin/customers')
    await fill()

    const password = screen.getByLabelText('Password') as HTMLInputElement
    expect(password.value).toBe('')
    fireEvent.click(screen.getByRole('button', { name: 'Generate' }))
    expect(password.value).toMatch(/^[A-Za-z0-9]{20}$/)
    const generated = password.value
    fireEvent.click(screen.getByRole('button', { name: 'Generate' }))
    expect(password.value).not.toBe(generated)

    fireEvent.click(screen.getByRole('button', { name: 'Create customer' }))
    await waitFor(() => expect(calls.some((call) => call.method === 'PUT')).toBe(true))

    const writes = calls.filter((call) => call.method !== 'GET')
    expect(writes.map((call) => `${call.method} ${call.url}`)).toEqual([
      'POST /v0/users',
      'POST /v0/organizations',
      'PUT /v0/subscriptions/eu-west',
    ])
    expect(writes[0]!.body).toEqual({ email: 'dev@acme.test', password: password.value, instance_admin: false })
    const org = writes[1]!.body as { slug: string; name: string; owner: string }
    expect(org.slug).toMatch(/^acme-corp-[a-z0-9]{4}$/)
    expect(org).toMatchObject({ name: 'Acme Corp', owner: 'dev@acme.test' })
    expect(writes[2]).toMatchObject({ org: org.slug, body: { plan: 'pro' } })
  })

  it('uses an existing account and says which step failed', async () => {
    const calls = stub({
      'POST /v0/users': () => json(409, { error: { code: 'conflict', message: 'a user with email "dev@acme.test" already exists' } }),
      'PUT /v0/subscriptions/eu-west': () => json(502, { error: { code: 'unavailable', message: 'region eu-west did not answer' } }),
    })
    renderApp('/admin/customers')
    await fill()
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'correct horse battery' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create customer' }))

    expect(await screen.findByText('Adding EU West failed:')).toBeInTheDocument()
    expect(screen.getByText('region eu-west did not answer')).toBeInTheDocument()
    expect(screen.getByText('Used the existing account dev@acme.test.')).toBeInTheDocument()
    expect(screen.getByText('Created the workspace Acme Corp.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Open Acme Corp' })).toBeInTheDocument()

    // Trying again only retries the region: the workspace isn't made twice.
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    await waitFor(() => expect(calls.filter((call) => call.method === 'PUT')).toHaveLength(2))
    expect(calls.filter((call) => call.method === 'POST' && call.url === '/v0/organizations')).toHaveLength(1)
  })
})

describe('customer regions', () => {
  it('adds a region with a plan and an override', async () => {
    const calls = stubAPI({
      ...detailRoutes(acme),
      'GET /v0/organizations?status=pending': () => json(200, { data: [] }),
      'PUT /v0/subscriptions/us-east': () => json(200, { ...euSubscription, region: usEast }),
    })
    renderApp('/admin/customers/acme')

    const row = (await screen.findByText('EU West')).closest('tr')!
    expect(within(row).getByText('Free')).toBeInTheDocument()
    expect(within(row).getByText('1 GB of 4 GB')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Add region' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByRole('combobox', { name: 'Region' })).toHaveTextContent('US East')
    await waitFor(() => expect(within(dialog).getByRole('combobox', { name: 'Plan' })).toHaveTextContent('Free'))
    fireEvent.change(within(dialog).getByLabelText('Sandboxes'), { target: { value: '5' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add region' }))

    await waitFor(() =>
      expect(calls.find((call) => call.method === 'PUT')).toMatchObject({
        url: '/v0/subscriptions/us-east',
        org: 'acme',
        body: { plan: 'free', max_sandboxes: 5, max_vcpus: null, max_memory_mb: null },
      }),
    )
  })

  it('removes a region, and offers to force it when keys can’t be revoked', async () => {
    const calls = stubAPI({
      ...detailRoutes(acme),
      'GET /v0/organizations?status=pending': () => json(200, { data: [] }),
      'DELETE /v0/subscriptions/eu-west': () =>
        json(502, { error: { code: 'unavailable', message: 'could not revoke 1 sandbox keys on "eu-west"' } }),
      'DELETE /v0/subscriptions/eu-west?force=true': () => json(200, euSubscription),
    })
    renderApp('/admin/customers/acme')

    fireEvent.click(await screen.findByRole('button', { name: 'Actions for EU West' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Remove region' }))
    expect(await screen.findByRole('heading', { name: 'Remove EU West?' })).toBeInTheDocument()
    expect(calls.some((call) => call.method === 'DELETE')).toBe(false)
    fireEvent.click(screen.getByRole('button', { name: 'Remove region' }))

    const force = await screen.findByRole('button', { name: 'Remove anyway' })
    expect(screen.getByText(/could not revoke 1 sandbox keys/)).toBeInTheDocument()
    fireEvent.click(force)
    await waitFor(() =>
      expect(calls.filter((call) => call.method === 'DELETE').map((call) => call.url)).toEqual([
        '/v0/subscriptions/eu-west',
        '/v0/subscriptions/eu-west?force=true',
      ]),
    )
    expect(calls.filter((call) => call.method === 'DELETE').every((call) => call.org === 'acme')).toBe(true)
  })
})
