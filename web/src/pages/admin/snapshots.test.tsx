import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { json, renderApp, stubAPI } from '../../test/render'

const euWest = { slug: 'eu-west', name: 'EU West', endpoint_url: 'https://eu.example', runtime: 'docker', status: 'ready' }

function snapshot(name: string, org: { slug: string; name: string }, node: string, diskGB: number, pushState = 'active') {
  return {
    name,
    region: 'eu-west',
    organization: org,
    node,
    source_sandbox_id: 'sb-1',
    image: 'ubuntu:22.04',
    created_at: '2026-11-12T12:00:00Z',
    cpu: 1,
    memory_mb: 1024,
    disk_gb: diskGB,
    push_state: pushState,
  }
}

const acme = { slug: 'acme', name: 'Acme' }
const globex = { slug: 'globex', name: 'Globex' }

describe('FleetSnapshotsPage', () => {
  it('totals every workspace’s snapshots, by node and workspace, and deletes in the owner’s workspace', async () => {
    const calls = stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/regions': () => json(200, { data: [euWest] }),
      'GET /v0/snapshots': () =>
        json(200, {
          data: [
            snapshot('ckpt-a', acme, 'node-a', 10),
            snapshot('ckpt-b', acme, 'node-b', 20, 'failed'),
            snapshot('ckpt-c', globex, 'node-a', 5),
          ],
          partial_nodes: [],
        }),
      'DELETE /v0/snapshots/ckpt-c': () => json(200, snapshot('ckpt-c', globex, 'node-a', 5)),
    })
    renderApp('/admin/snapshots')

    expect(await screen.findByRole('heading', { name: 'Snapshots', level: 1 })).toBeInTheDocument()
    expect(await screen.findByText('35 GB')).toBeInTheDocument()
    expect(screen.getByText('3 GB')).toBeInTheDocument() // memory
    expect(screen.getByText('across 2 workspaces')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /node-a \(EU West\) · 2 snapshots/ })).toHaveAttribute(
      'href',
      '/admin/servers/clusters/eu-west',
    )
    expect(screen.getByRole('link', { name: /Globex · 1 snapshot\b/ })).toHaveAttribute('href', '/admin/workspaces/globex')

    const row = screen.getByRole('row', { name: /ckpt-c/ })
    expect(within(row).getByText('Globex')).toBeInTheDocument()
    expect(within(row).getByText('node-a')).toBeInTheDocument()
    expect(screen.getByText('Upload failed')).toBeInTheDocument()

    fireEvent.click(within(row).getByRole('button', { name: 'Delete ckpt-c' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete snapshot' }))
    await waitFor(() => expect(screen.queryByRole('row', { name: /ckpt-c/ })).not.toBeInTheDocument())
    const deleted = calls.find((call) => call.method === 'DELETE')
    expect(deleted).toMatchObject({ url: '/v0/snapshots/ckpt-c?region=eu-west', org: 'globex' })
  })

  it('lists every workspace’s archives on the Archived tab, with delete only', async () => {
    const archive = {
      id: '01a1cb34-29a5-74c3-9691-5394d021b0aa',
      org: 'globex',
      region: 'eu-west',
      sandbox_id: 'sb-1',
      sandbox_name: 'builder',
      image: 'ubuntu:22.04',
      runtime: 'docker',
      cpu: 1,
      memory_mb: 1024,
      disk_gb: 10,
      tags: {},
      snapshot_name: 'a',
      registry_ref: '',
      status: 'archived',
      error: '',
      created_at: '2026-11-12T10:00:00Z',
      archived_at: '2026-11-12T10:05:00Z',
      last_restored_sandbox_id: '',
      last_restored_at: null,
    }
    const calls = stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/regions': () => json(200, { data: [euWest] }),
      'GET /v0/archives': () => json(200, { data: [archive] }),
      [`DELETE /v0/archives/${archive.id}`]: () => json(200, archive),
    })
    renderApp('/admin/snapshots?tab=archived')

    const row = (await screen.findByText('builder')).closest('tr')!
    expect(within(row).getByText('globex')).toBeInTheDocument()
    expect(within(row).getByText('EU West')).toBeInTheDocument()
    expect(within(row).queryByRole('button', { name: /Restore/ })).toBeNull()
    fireEvent.click(within(row).getByRole('button', { name: 'Delete builder' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete archive' }))
    await waitFor(() =>
      expect(calls.find((call) => call.method === 'DELETE')).toMatchObject({ org: 'globex' }),
    )
  })
})
