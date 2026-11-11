import { fireEvent, screen, within } from '@testing-library/react'
import { Route } from 'react-router'
import { beforeEach, describe, expect, it } from 'vitest'
import { json, renderAt, stubAPI } from '../../test/render'
import { ServersPage } from './servers'

const cluster = {
  id: '01a1cb34-29a5-74c3-9691-5394d021b0f2',
  slug: 'eu-west',
  name: 'EU West',
  admin_url: 'http://10.0.0.5:21212',
  endpoint_url: 'http://10.0.0.5:21212',
  runtime: 'docker',
  admin_token_last4: '9f3a',
  status: 'ready',
  version: 'v0.7.0',
  nodes: 1,
  checked_at: '2026-11-11T15:23:01.2Z',
  created_at: '2026-11-11T15:22:35.8Z',
}

const node = {
  id: '01a1cb34-29a5-74c3-9691-5394d021b0f3',
  name: 'node-1',
  url: 'http://10.0.0.5:21212',
  created_at: '2026-11-11T15:22:35.8Z',
  cluster: 'eu-west',
  role: 'mixed',
  version: 'v0.7.0',
  alive: true,
}

beforeEach(() => {
  stubAPI({
    'GET /v0/organizations': () => json(200, { data: [] }),
    'GET /v0/clusters': () => json(200, { data: [cluster] }),
    'GET /v0/nodes': () => json(200, { data: [node] }),
  })
})

describe('ServersPage', () => {
  it('shows clusters, and nodes in their own tab', async () => {
    renderAt('/admin/servers', <Route path="/admin/servers" element={<ServersPage />} />)
    // Every cell of a linked row is a link to the cluster.
    expect((await screen.findAllByRole('link', { name: /EU West/ }))[0]).toHaveAttribute(
      'href',
      '/admin/servers/clusters/eu-west',
    )
    const tabs = screen.getByRole('navigation', { name: 'Server views' })
    expect(within(tabs).getByRole('link', { name: 'Clusters' })).toHaveAttribute('aria-current', 'page')

    fireEvent.click(within(tabs).getByRole('link', { name: 'Nodes' }))
    expect(await screen.findByText('node-1')).toBeInTheDocument()
    expect(within(tabs).getByRole('link', { name: 'Nodes' })).toHaveAttribute('aria-current', 'page')
  })

  it('offers every way to add a server to instance admins', async () => {
    renderAt('/admin/servers', <Route path="/admin/servers" element={<ServersPage />} />)
    fireEvent.click(await screen.findByRole('button', { name: /Add server/ }))
    expect(await screen.findByRole('menuitem', { name: /Start a new cluster/ })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: /Add a node to a cluster/ })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: /Register an existing cluster/ })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: /Register a node by URL/ })).toBeInTheDocument()
  })
})
