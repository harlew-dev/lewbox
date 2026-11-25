import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type * as api from '../../lib/api'
import { json, renderApp, stubAPI } from '../../test/render'

const euWest = { slug: 'eu-west', name: 'EU West', endpoint_url: 'https://eu.example', runtime: 'docker', status: 'ready' }

function snapshot(
  name: string,
  org: { slug: string; name: string },
  node: string,
  diskGB: number,
  status = 'active',
  extra: Partial<api.Snapshot> = {},
) {
  return {
    id: '',
    kind: 'snapshot',
    name,
    region: 'eu-west',
    organization: org,
    node,
    sandbox_name: '',
    source_sandbox_id: 'sb-1',
    image: 'ubuntu:22.04',
    runtime: 'docker',
    created_at: '2026-11-12T12:00:00Z',
    archived_at: null,
    cpu: 1,
    memory_mb: 1024,
    disk_gb: diskGB,
    tags: {},
    status,
    error: '',
    registry_ref: '',
    last_restored_sandbox_id: '',
    last_restored_at: null,
    ...extra,
  }
}

const acme = { slug: 'acme', name: 'Acme' }
const globex = { slug: 'globex', name: 'Globex' }

describe('FleetSnapshotsPage', () => {
  it('filters by kind, region and workspace from the URL', async () => {
    stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/regions': () =>
        json(200, {
          data: [
            euWest,
            { slug: 'us-east', name: 'US East', endpoint_url: 'https://us.example', runtime: 'docker', status: 'ready' },
          ],
        }),
      'GET /v0/snapshots': () =>
        json(200, {
          data: [
            snapshot('builder', globex, 'node-a', 10, 'active', {
              kind: 'archive',
              sandbox_name: 'builder',
              archived_at: '2026-11-12T10:05:00Z',
            }),
          ],
          partial_nodes: [],
          summary: {
            count: 1,
            disk_gb: 10,
            memory_mb: 1024,
            failed: 0,
            workspaces: 1,
            by_node: [],
            by_workspace: [],
          },
        }),
    })
    renderApp('/admin/snapshots?kind=archive&region=eu-west&organization=globex')

    expect(await screen.findByText('builder')).toBeInTheDocument()
    expect(screen.getByText('10 GB')).toBeInTheDocument()
  })

  it('totals every workspace’s snapshots, by node and workspace, and deletes in the owner’s workspace', async () => {
    const calls = stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/regions': () => json(200, { data: [euWest] }),
      'GET /v0/snapshots': () =>
        json(200, {
          data: [
            snapshot('ckpt-a', acme, 'node-a', 10),
            snapshot('ckpt-b', acme, 'node-b', 20, 'error'),
            snapshot('ckpt-c', globex, 'node-a', 5),
          ],
          partial_nodes: [],
          summary: {
            count: 3,
            disk_gb: 35,
            memory_mb: 3072,
            failed: 1,
            workspaces: 2,
            by_node: [
              { region: 'eu-west', node: 'node-a', kind: 'snapshot', count: 2, disk_gb: 15 },
              { region: 'eu-west', node: 'node-b', kind: 'snapshot', count: 1, disk_gb: 20 },
            ],
            by_workspace: [
              { slug: 'acme', name: 'Acme', count: 2, disk_gb: 30 },
              { slug: 'globex', name: 'Globex', count: 1, disk_gb: 5 },
            ],
          },
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
    expect(within(screen.getByRole('row', { name: /ckpt-b/ })).getByText('Error')).toBeInTheDocument()

    fireEvent.click(within(row).getByRole('button', { name: 'Delete ckpt-c' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete snapshot' }))
    await waitFor(() => expect(screen.queryByRole('row', { name: /ckpt-c/ })).not.toBeInTheDocument())
    const deleted = calls.find((call) => call.method === 'DELETE')
    expect(deleted).toMatchObject({ url: '/v0/snapshots/ckpt-c?region=eu-west', org: 'globex' })
  })

  it('lists archives in the same table, with delete only for the operator', async () => {
    const archive = {
      id: '01a1cb34-29a5-74c3-9691-5394d021b0aa',
      kind: 'archive',
      name: 'archive-1',
      region: 'eu-west',
      organization: globex,
      sandbox_name: 'builder',
      source_sandbox_id: 'sb-1',
      image: 'ubuntu:22.04',
      runtime: 'docker',
      created_at: '2026-11-12T10:00:00Z',
      archived_at: '2026-11-12T10:05:00Z',
      cpu: 1,
      memory_mb: 1024,
      disk_gb: 10,
      tags: {},
      status: 'active',
      error: '',
      registry_ref: '',
      last_restored_sandbox_id: '',
      last_restored_at: null,
    }
    const calls = stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/regions': () => json(200, { data: [euWest] }),
      'GET /v0/snapshots': () =>
        json(200, {
          data: [archive],
          partial_nodes: [],
          summary: {
            count: 1,
            disk_gb: 10,
            memory_mb: 1024,
            failed: 0,
            workspaces: 1,
            by_node: [
              { region: 'eu-west', node: null, kind: 'archive', count: 1, disk_gb: 10 },
            ],
            by_workspace: [{ slug: 'globex', name: 'Globex', count: 1, disk_gb: 10 }],
          },
        }),
      [`DELETE /v0/archives/${archive.id}`]: () => json(200, archive),
    })
    renderApp('/admin/snapshots')

    const row = (await screen.findByText('builder')).closest('tr')!
    expect(within(row).getByText('Archived sandbox')).toBeInTheDocument()
    expect(within(row).getByText('Globex')).toBeInTheDocument()
    expect(within(row).getByText('EU West')).toBeInTheDocument()
    expect(within(row).queryByRole('button', { name: /Restore/ })).toBeNull()
    expect(screen.getByRole('link', { name: /Archived to storage \(EU West\) · 1 snapshot/ })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Archived' })).toBeNull()

    fireEvent.click(within(row).getByRole('button', { name: 'Delete builder' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete archive' }))
    await waitFor(() =>
      expect(calls.find((call) => call.method === 'DELETE')).toMatchObject({ org: 'globex' }),
    )
  })
})
