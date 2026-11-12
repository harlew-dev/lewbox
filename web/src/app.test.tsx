import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { legacyPath } from './app'
import { acme, acmeMembership, customerUser, json, renderApp, stubAPI } from './test/render'

const overview = {
  customers: { total: 3, pending: 1, suspended: 0 },
  sandboxes: { running: 2, stopped: 1 },
  capacity: { nodes_alive: 1, nodes: 1, cpu_cores: 4, cpu_reserved: 2, memory_total_mb: 8192, memory_used_mb: 2048 },
  clusters: [],
  running: [],
  top_customers: [],
}

describe('routing', () => {
  it('shows a customer the portal and never the console', async () => {
    stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/usage': () =>
          json(200, { from: '', to: '', granularity: 'day', totals: { sandbox_hours: 0, vcpu_hours: 0, memory_gib_hours: 0 }, data: [] }),
        'GET /v0/sandboxes': () => json(200, { data: [] }),
        'GET /v0/sandbox-keys': () => json(200, { data: [] }),
        'GET /v0/regions': () => json(200, { data: [] }),
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/')

    expect(await screen.findByRole('heading', { name: 'Get started' })).toBeInTheDocument()
    const nav = screen.getAllByRole('link', { name: 'Sandboxes' })[0]!.closest('nav')!
    const links = within(nav).getAllByRole('link').map((link) => link.textContent)
    expect(links.filter((label) => ['Home', 'Sandboxes', 'API keys', 'Usage', 'Plan & Billing'].includes(label ?? ''))).toEqual([
      'Home',
      'Sandboxes',
      'API keys',
      'Usage',
      'Plan & Billing',
    ])
    expect(within(nav).getByText('Manage')).toBeInTheDocument()
    expect(links).not.toContain('Snapshots')
    expect(links).not.toContain('Team')
    expect(within(nav).getByRole('link', { name: 'API keys' })).toHaveAttribute('href', '/keys')
    expect(within(nav).getByRole('link', { name: 'Plan & Billing' })).toHaveAttribute('href', '/plan')
    expect(within(nav).queryByRole('link', { name: 'Customers' })).toBeNull()
    expect(within(nav).queryByRole('link', { name: 'Plans' })).toBeNull()
    expect(within(nav).queryByRole('link', { name: 'Servers' })).toBeNull()
    expect(await screen.findByText('Owner')).toBeInTheDocument()
  })

  it('sends the old Snapshots, Team and Settings pages to their new places', async () => {
    stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/regions': () => json(200, { data: [] }),
        'GET /v0/snapshots': () => json(200, { data: [], partial_nodes: [] }),
        'GET /v0/members': () => json(200, { data: [] }),
      },
      customerUser,
      [acmeMembership],
    )
    const snapshots = renderApp('/snapshots')
    const tabs = await screen.findByRole('navigation', { name: 'Sandbox views' })
    expect(within(tabs).getByRole('link', { name: 'Snapshots' })).toHaveAttribute('aria-current', 'page')
    expect(await screen.findByText('Snapshots you take show up here.')).toBeInTheDocument()
    snapshots.unmount()

    renderApp('/settings')
    expect(await screen.findByRole('heading', { name: 'Workspace settings' })).toBeInTheDocument()
    expect(await screen.findByLabelText('Workspace name')).toHaveValue('Acme')
  })

  it('sends the old Team page to the Members settings tab', async () => {
    stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/members': () => json(200, { data: [] }),
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/team')
    expect(await screen.findByRole('heading', { name: 'Workspace settings' })).toBeInTheDocument()
    const tabs = screen.getByRole('navigation', { name: 'Workspace settings' })
    expect(within(tabs).getByRole('link', { name: 'Members' })).toHaveAttribute('aria-current', 'page')
  })

  it('gives a customer the not-found page for the console', async () => {
    stubAPI({}, customerUser, [acmeMembership])
    renderApp('/admin/workspaces')
    expect(await screen.findByRole('heading', { name: 'This page has wandered off.' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Workspaces' })).toBeNull()
  })

  it('sends an instance admin without a workspace to the console', async () => {
    stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/overview': () => json(200, overview),
    })
    renderApp('/')
    expect(await screen.findByRole('heading', { name: 'Overview' })).toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: /Workspaces/ })[0]).toHaveAttribute('href', '/admin/workspaces')
    expect(screen.getAllByRole('link', { name: /Plans/ })[0]).toHaveAttribute('href', '/admin/plans')
  })

  it('maps the old console paths under /admin', () => {
    expect(legacyPath('/servers', '?tab=nodes')).toBe('/admin/servers?tab=nodes')
    expect(legacyPath('/servers/clusters/eu-west', '')).toBe('/admin/servers/clusters/eu-west')
    expect(legacyPath('/clusters/eu-west', '')).toBe('/admin/servers/clusters/eu-west')
    expect(legacyPath('/nodes', '')).toBe('/admin/servers?tab=nodes')
    expect(legacyPath('/users/abc', '')).toBe('/admin/users/abc')
    expect(legacyPath('/organizations/acme/members', '')).toBe('/admin/workspaces/acme')
    expect(legacyPath('/organizations', '')).toBe('/admin/workspaces')
  })
})
