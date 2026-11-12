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
      error: '',
    },
  ],
}

// What the workspace page loads, for acme.
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

describe('workspaces', () => {
  it('lists workspaces with their cluster and plan, and a tab for those waiting', async () => {
    stubAPI({
      'GET /v0/organizations': () => json(200, { data: [pending] }),
      'GET /v0/sandboxes?status=started': () => json(200, { data: [] }),
    })
    renderApp('/admin/workspaces?tab=pending')
    const row = (await screen.findAllByRole('link', { name: /Acme/ }))[0]!
    expect(row).toHaveAttribute('href', '/admin/workspaces/acme')
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeInTheDocument()
    expect(screen.getByText('EU West')).toBeInTheDocument()
    expect(screen.getByText('Free')).toBeInTheDocument()
    expect(screen.queryByText(/eu-west/)).toBeNull()
    expect(screen.getByText('12.5')).toBeInTheDocument()
    const tabs = screen.getByRole('navigation', { name: 'Workspace views' })
    expect(within(tabs).getByRole('link', { name: 'Pending' })).toHaveAttribute('aria-current', 'page')
  })

  it('approves a pending workspace', async () => {
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
    renderApp('/admin/workspaces/acme')

    fireEvent.click(await screen.findByRole('button', { name: 'Approve' }))
    await waitFor(() =>
      expect(calls.find((call) => call.method === 'PATCH')).toMatchObject({
        url: '/v0/organizations/acme',
        body: { status: 'active' },
      }),
    )
    expect(await screen.findByRole('button', { name: 'Suspend' })).toBeInTheDocument()
  })

  it('offers View and Suspend in the row menu, and Suspend opens the dialog', async () => {
    stubAPI({
      'GET /v0/organizations': () => json(200, { data: [acme] }),
      'GET /v0/sandboxes?status=started': () => json(200, { data: [] }),
    })
    renderApp('/admin/workspaces')

    fireEvent.click(await screen.findByRole('button', { name: 'Workspace actions' }))
    expect(await screen.findByRole('menuitem', { name: 'View' })).toHaveAttribute('href', '/admin/workspaces/acme')
    expect(screen.queryByRole('menuitem', { name: 'Restore' })).toBeNull()
    fireEvent.click(screen.getByRole('menuitem', { name: 'Suspend' }))

    expect(await screen.findByRole('heading', { name: 'Suspend Acme?' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Suspend workspace' })).toBeInTheDocument()
  })

  it('opens a workspace from its row menu', async () => {
    stubAPI({
      ...detailRoutes(acme),
      'GET /v0/organizations': () => json(200, { data: [acme] }),
      'GET /v0/sandboxes?status=started': () => json(200, { data: [] }),
    })
    renderApp('/admin/workspaces')

    fireEvent.click(await screen.findByRole('button', { name: 'Workspace actions' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'View' }))

    expect(await screen.findByRole('heading', { name: 'Acme' })).toBeInTheDocument()
    expect(screen.getByRole('navigation', { name: 'Workspace views' })).toBeInTheDocument()
  })
})

describe('new workspace', () => {
  const settings = { instance_name: 'Lewbox', signup: 'closed', default_plan: 'pro', default_region: 'eu-west' }
  const user = { id: 'u1', email: 'dev@acme.test', instance_admin: false, disabled: false, created_at: '' }
  const noAccount = () => json(404, { error: { code: 'not_found', message: 'no user "dev@acme.test"' } })

  function stub(routes: Record<string, (call: Call) => Response>) {
    return stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/sandboxes?status=started': () => json(200, { data: [] }),
      'GET /v0/settings': () => json(200, settings),
      'GET /v0/regions': () => json(200, { data: [euWest, usEast] }),
      'GET /v0/plans': () => json(200, { data: [free, pro] }),
      'POST /v0/organizations': (call) => {
        const body = call.body as { name: string }
        return json(201, { ...acme, slug: 'acme-corp-abc123', name: body.name, role: undefined, region: null })
      },
      ...routes,
    })
  }

  async function fill() {
    fireEvent.click(await screen.findByRole('button', { name: 'New workspace' }))
    fireEvent.change(await screen.findByLabelText('Owner email'), { target: { value: 'dev@acme.test' } })
    fireEvent.change(screen.getByLabelText('Workspace name'), { target: { value: 'Acme Corp' } })
    // The default plan in the default region is filled in.
    expect(await screen.findByRole('combobox', { name: 'Cluster' })).toHaveTextContent('EU West')
    expect(screen.getByRole('combobox', { name: 'Plan' })).toHaveTextContent('Pro')
  }

  const writes = (calls: Call[]) => calls.filter((call) => call.method !== 'GET').map((call) => `${call.method} ${call.url}`)

  it('generates a password, then creates the account, the workspace and its cluster plan in order', async () => {
    const calls = stub({
      'GET /v0/users/dev%40acme.test': noAccount,
      'POST /v0/users': () => json(201, user),
      'PUT /v0/subscriptions/eu-west': () => json(200, { ...euSubscription, plan: { slug: 'pro', name: 'Pro' } }),
    })
    renderApp('/admin/workspaces')
    await fill()

    const password = screen.getByLabelText('Password') as HTMLInputElement
    expect(password.value).toBe('')
    fireEvent.click(screen.getByRole('button', { name: 'Generate' }))
    expect(password.value).toMatch(/^[A-Za-z0-9]{20}$/)
    const generated = password.value
    fireEvent.click(screen.getByRole('button', { name: 'Generate' }))
    expect(password.value).not.toBe(generated)

    fireEvent.click(screen.getByRole('button', { name: 'Create workspace' }))
    await waitFor(() => expect(calls.some((call) => call.method === 'PUT')).toBe(true))

    expect(writes(calls)).toEqual(['POST /v0/users', 'POST /v0/organizations', 'PUT /v0/subscriptions/eu-west'])
    const sent = calls.filter((call) => call.method !== 'GET')
    expect(sent[0]!.body).toEqual({ email: 'dev@acme.test', password: password.value, instance_admin: false })
    const org = sent[1]!.body as { slug?: string; name: string; owner: string }
    expect(org.slug).toBeUndefined()
    expect(org).toMatchObject({ name: 'Acme Corp', owner: 'dev@acme.test' })
    expect(sent[2]).toMatchObject({ org: 'acme-corp-abc123', body: { plan: 'pro' } })
  })

  it('uses an existing account without a password, and says which step failed', async () => {
    const calls = stub({
      'GET /v0/users/dev%40acme.test': () => json(200, user),
      'PUT /v0/subscriptions/eu-west': () => json(502, { error: { code: 'unavailable', message: 'cluster eu-west did not answer' } }),
    })
    renderApp('/admin/workspaces')
    await fill()
    // No password: the account exists, so none is needed.
    fireEvent.click(screen.getByRole('button', { name: 'Create workspace' }))

    expect(await screen.findByText('Adding EU West failed:')).toBeInTheDocument()
    expect(screen.getByText('cluster eu-west did not answer')).toBeInTheDocument()
    expect(screen.getByText('Used the existing account dev@acme.test.')).toBeInTheDocument()
    expect(screen.getByText('Created the workspace Acme Corp.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Open Acme Corp' })).toBeInTheDocument()
    expect(calls.some((call) => call.method === 'POST' && call.url === '/v0/users')).toBe(false)

    // Trying again only retries the plan: the workspace isn't made twice.
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    await waitFor(() => expect(calls.filter((call) => call.method === 'PUT')).toHaveLength(2))
    expect(calls.filter((call) => call.method === 'POST' && call.url === '/v0/organizations')).toHaveLength(1)
  })

  it('asks for a password when the email has no account', async () => {
    const calls = stub({ 'GET /v0/users/dev%40acme.test': noAccount })
    renderApp('/admin/workspaces')
    await fill()
    fireEvent.click(screen.getByRole('button', { name: 'Create workspace' }))

    expect(await screen.findByText('Creating the account failed:')).toBeInTheDocument()
    expect(screen.getByText('dev@acme.test has no account yet. Enter a password for it.')).toBeInTheDocument()
    expect(writes(calls)).toEqual([])
    expect(screen.getByLabelText('Password')).toBeEnabled()
  })

  it('remembers the account it made, and locks what was made so a retry can’t ignore an edit', async () => {
    let orgFails = true
    const calls = stub({
      'GET /v0/users/dev%40acme.test': noAccount,
      'POST /v0/users': () => json(201, user),
      'POST /v0/organizations': (call) => {
        if (orgFails) {
          orgFails = false
          return json(500, { error: { code: 'internal', message: 'try later' } })
        }
        const body = call.body as { name: string }
        return json(201, { ...acme, slug: 'acme-corp-abc123', name: body.name, role: undefined, region: null })
      },
      'PUT /v0/subscriptions/eu-west': () => json(502, { error: { code: 'unavailable', message: 'cluster eu-west did not answer' } }),
    })
    renderApp('/admin/workspaces')
    await fill()
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'correct horse battery' } })

    fireEvent.click(screen.getByRole('button', { name: 'Create workspace' }))
    expect(await screen.findByText('Creating the workspace failed:')).toBeInTheDocument()
    expect(screen.getByText('Created the account dev@acme.test.')).toBeInTheDocument()
    expect(screen.getByLabelText('Owner email')).toBeDisabled()
    expect(screen.getByLabelText('Password')).toBeDisabled()
    expect(screen.getByLabelText('Workspace name')).toBeEnabled()

    // The retry doesn't make the account again, nor call it "existing".
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByText('Adding EU West failed:')).toBeInTheDocument()
    expect(calls.filter((call) => call.method === 'PUT')).toHaveLength(1)
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    await waitFor(() => expect(calls.filter((call) => call.method === 'PUT')).toHaveLength(2))
    expect(screen.getByText('Created the account dev@acme.test.')).toBeInTheDocument()
    expect(screen.queryByText(/Used the existing account/)).toBeNull()
    expect(calls.filter((call) => call.method === 'POST' && call.url === '/v0/users')).toHaveLength(1)
    expect(screen.getByLabelText('Workspace name')).toBeDisabled()
    // Its plan wasn't added, so its cluster stays editable.
    expect(screen.getByRole('combobox', { name: 'Cluster' })).not.toHaveAttribute('data-disabled')
  })

  it('refreshes the workspaces when the dialog closes after making a workspace', async () => {
    const calls = stub({
      'GET /v0/users/dev%40acme.test': () => json(200, user),
      'PUT /v0/subscriptions/eu-west': () => json(502, { error: { code: 'unavailable', message: 'cluster eu-west did not answer' } }),
    })
    renderApp('/admin/workspaces')
    await fill()
    fireEvent.click(screen.getByRole('button', { name: 'Create workspace' }))
    expect(await screen.findByText('Adding EU West failed:')).toBeInTheDocument()

    const lists = () => calls.filter((call) => call.method === 'GET' && call.url === '/v0/organizations').length
    const before = lists()
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    await waitFor(() => expect(lists()).toBe(before + 1))
  })
})

describe('workspace cluster and plan', () => {
  it('assigns a cluster with a plan and an override', async () => {
    const calls = stubAPI({
      ...detailRoutes({ ...acme, region: null }),
      'GET /v0/subscriptions': () => json(200, { data: [] }),
      'GET /v0/limits': () => json(200, { regions: [] }),
      'PUT /v0/subscriptions/us-east': () => json(200, { ...euSubscription, region: usEast }),
    })
    renderApp('/admin/workspaces/acme')

    expect(await screen.findByText('No cluster. Acme can’t run sandboxes until you assign one.')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Assign cluster' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByRole('combobox', { name: 'Cluster' })).toHaveTextContent('EU West')
    await waitFor(() => expect(within(dialog).getByRole('combobox', { name: 'Plan' })).toHaveTextContent('Free'))
    fireEvent.click(within(dialog).getByRole('combobox', { name: 'Cluster' }))
    const usEastOption = await screen.findByRole('option', { name: 'US East' })
    fireEvent.pointerDown(usEastOption)
    fireEvent.click(usEastOption)
    await waitFor(() => expect(within(dialog).getByRole('combobox', { name: 'Cluster' })).toHaveTextContent('US East'))
    fireEvent.change(within(dialog).getByLabelText('Sandboxes'), { target: { value: '5' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Assign' }))

    await waitFor(() =>
      expect(calls.find((call) => call.method === 'PUT')).toMatchObject({
        url: '/v0/subscriptions/us-east',
        org: 'acme',
        body: { plan: 'free', max_sandboxes: 5, max_vcpus: null, max_memory_mb: null },
      }),
    )
  })

  it('removes a cluster, and offers to force it when keys can’t be revoked', async () => {
    const calls = stubAPI({
      ...detailRoutes(acme),
      'DELETE /v0/subscriptions/eu-west': () =>
        json(502, { error: { code: 'unavailable', message: 'could not revoke 1 sandbox keys on "eu-west"' } }),
      'DELETE /v0/subscriptions/eu-west?force=true': () => json(200, euSubscription),
    })
    renderApp('/admin/workspaces/acme')

    fireEvent.click(await screen.findByRole('button', { name: 'Actions for EU West' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Remove cluster' }))
    expect(await screen.findByRole('heading', { name: 'Remove EU West?' })).toBeInTheDocument()
    expect(calls.some((call) => call.method === 'DELETE')).toBe(false)
    fireEvent.click(screen.getByRole('button', { name: 'Remove cluster' }))

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

  it('offers no forced removal when the failure isn’t about revoking keys', async () => {
    const calls = stubAPI({
      ...detailRoutes(acme),
      'DELETE /v0/subscriptions/eu-west': () =>
        json(502, { error: { code: 'unavailable', message: 'cluster eu-west did not answer' } }),
    })
    renderApp('/admin/workspaces/acme')

    fireEvent.click(await screen.findByRole('button', { name: 'Actions for EU West' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Remove cluster' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Remove cluster' }))

    expect(await screen.findByText(/cluster eu-west did not answer/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Remove anyway' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Remove cluster' })).toBeInTheDocument()
    expect(calls.filter((call) => call.method === 'DELETE')).toHaveLength(1)
  })

  it('says when a cluster’s quota couldn’t be pushed', async () => {
    stubAPI({
      ...detailRoutes(acme),
      'GET /v0/limits': () =>
        json(200, { regions: [{ ...limits.regions[0], error: 'eu-west: connection refused' }] }),
    })
    renderApp('/admin/workspaces/acme')

    const row = (await screen.findByText('EU West')).closest('tr')!
    expect(await within(row).findByText('Quota push failed: eu-west: connection refused')).toBeInTheDocument()
  })

  it('has no Snapshots tab', async () => {
    stubAPI({ ...detailRoutes(acme) })
    renderApp('/admin/workspaces/acme')
    const tabs = await screen.findByRole('navigation', { name: 'Workspace views' })
    expect(within(tabs).getAllByRole('link').map((link) => link.textContent)).toEqual([
      'Overview',
      'Sandboxes',
      'API keys',
      'Members',
    ])
  })
})

describe('setup links', () => {
  const link = {
    id: 'sl1',
    plan: { slug: 'pro', name: 'Pro' },
    region: { slug: 'eu-west', name: 'EU West' },
    note: 'For Acme',
    status: 'unused',
    claimed_by: null,
    workspace: null,
    created_at: '2026-11-12T10:00:00Z',
    expires_at: '2026-11-19T10:00:00Z',
    used_at: null,
  }
  const used = {
    ...link,
    id: 'sl2',
    status: 'used',
    note: 'For Beta',
    claimed_by: 'dev@acme.test',
    workspace: { slug: 'acme', name: 'Acme' },
    used_at: '2026-11-13T10:00:00Z',
  }
  const settings = { instance_name: 'Lewbox', signup: 'closed', default_plan: 'pro', default_region: 'eu-west' }

  function setupRoutes(routes: Record<string, (call: Call) => Response> = {}) {
    return stubAPI({
      'GET /v0/organizations?status=pending': () => json(200, { data: [] }),
      'GET /v0/setup-links': () => json(200, { data: [link, used] }),
      'GET /v0/settings': () => json(200, settings),
      'GET /v0/regions': () => json(200, { data: [euWest, usEast] }),
      'GET /v0/plans': () => json(200, { data: [free, pro] }),
      ...routes,
    })
  }

  it('lists links and shows a new one’s URL once', async () => {
    let created = false
    const calls = setupRoutes({
      'GET /v0/setup-links': () => json(200, { data: created ? [link] : [] }),
      'POST /v0/setup-links': (call) => {
        created = true
        return json(201, { ...link, token: 'lbs_new', note: (call.body as { note: string }).note })
      },
    })
    renderApp('/admin/workspaces?tab=setup')

    const tabs = await screen.findByRole('navigation', { name: 'Workspace views' })
    expect(within(tabs).getByRole('link', { name: 'Setup links' })).toHaveAttribute('aria-current', 'page')
    fireEvent.click(await screen.findByRole('button', { name: 'Create setup link' }))

    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByRole('combobox', { name: 'Cluster' })).toHaveTextContent('EU West')
    await waitFor(() => expect(within(dialog).getByRole('combobox', { name: 'Plan' })).toHaveTextContent('Pro'))
    fireEvent.change(within(dialog).getByLabelText('Note'), { target: { value: 'For Acme' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create setup link' }))

    expect(await screen.findByText(/Send this link to your customer\. It works once, until/)).toBeInTheDocument()
    expect(screen.getByText(/\/setup\/lbs_new$/)).toBeInTheDocument()
    const post = calls.find((call) => call.method === 'POST')
    expect(post?.body).toEqual({ plan: 'pro', region: 'eu-west', expires_in_hours: 168, note: 'For Acme' })
    expect(calls.some((call) => call.url === '/v0/organizations')).toBe(false)
    expect(calls.some((call) => call.url.startsWith('/v0/sandboxes'))).toBe(false)
  })

  it('revokes a link after confirmation and links a used one to its workspace', async () => {
    let revoked = false
    const calls = setupRoutes({
      'GET /v0/setup-links': () => json(200, { data: revoked ? [used] : [link, used] }),
      'DELETE /v0/setup-links/sl1': () => {
        revoked = true
        return json(200, link)
      },
    })
    renderApp('/admin/workspaces?tab=setup')

    expect(await screen.findByRole('link', { name: 'Acme' })).toHaveAttribute('href', '/admin/workspaces/acme')
    expect(screen.getByText('dev@acme.test')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Setup link actions' })).toHaveLength(1)

    fireEvent.click(screen.getByRole('button', { name: 'Setup link actions' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Revoke' }))
    expect(await screen.findByRole('heading', { name: 'Revoke setup link?' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Revoke' }))

    await waitFor(() => expect(calls.some((call) => call.method === 'DELETE')).toBe(true))
    expect(calls.find((call) => call.method === 'DELETE')?.url).toBe('/v0/setup-links/sl1')
    await waitFor(() => expect(screen.queryByText('For Acme')).toBeNull())
  })
})
