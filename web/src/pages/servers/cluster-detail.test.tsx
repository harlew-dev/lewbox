import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { Route } from 'react-router'
import { describe, expect, it } from 'vitest'
import { choose, json, renderAt, stubAPI } from '../../test/render'
import { ClusterDetailPage } from './cluster-detail'

const cluster = {
  id: 'c1',
  slug: 'eu-west',
  name: 'EU West',
  admin_url: 'http://10.0.0.5:21212',
  endpoint_url: 'http://10.0.0.5:21212',
  runtime: 'docker',
  domain: 'eu.api.example.com',
  dns_profile: 'cf-prod',
  dns_records: 'auto',
  storage_profile: null,
  stop_policy: 'keep',
  archive_local_retention: 'instant',
  archive_snapshot_name: 'id',
  admin_token_last4: '9f3a',
  status: 'ready',
  version: 'v0.7.0',
  nodes: 2,
  checked_at: '2026-11-11T15:23:01.2Z',
  created_at: '2026-11-11T15:22:35.8Z',
  checks: [],
  node_list: [
    {
      id: 'n1', name: 'node-1', url: 'http://10.0.0.5:21212', created_at: '', cluster: 'eu-west', role: 'mixed', alive: true,
      settings: { applied_at: '2026-11-12T10:00:00Z', outdated: true, reasons: ['storage profile s3-main is at version 2, the node has 1'] },
    },
    {
      id: 'n2', name: 'node-2', url: 'http://10.0.0.6:21212', created_at: '', cluster: 'eu-west', role: 'worker', alive: true,
      settings: { applied_at: '2026-11-12T10:00:00Z', outdated: false, reasons: [] },
    },
  ],
}

const profiles = {
  'GET /v0/dns-profiles': () =>
    json(200, { data: [{ id: 'd1', name: 'cf-prod', zone: 'example.com' }] }),
  'GET /v0/storage-profiles': () =>
    json(200, { data: [{ id: 's1', name: 's3-main', bucket: 'lewbox' }] }),
}

function open() {
  return renderAt('/admin/servers/clusters/eu-west', <Route path="/admin/servers/clusters/:ref" element={<ClusterDetailPage />} />)
}

describe('ClusterDetailPage settings', () => {
  it('shows the settings and sends only what changed, then asks for node configure', async () => {
    const calls = stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/clusters/eu-west': () => json(200, cluster),
      'PATCH /v0/clusters/eu-west': () => json(200, cluster),
      ...profiles,
    })
    open()
    expect(await screen.findByRole('heading', { name: 'Settings' })).toBeInTheDocument()
    expect(screen.getByText('eu.api.example.com', { selector: 'code' })).toBeInTheDocument()
    expect(screen.getByText('Keep it on its server')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
    expect(screen.getByRole('button', { name: 'Save settings' })).toBeDisabled()
    // No storage profile yet: archiving is off, with the reason.
    expect(screen.getByText(/Not available: Choose a storage profile first/)).toBeInTheDocument()
    expect(screen.queryByLabelText('Local copies on servers')).toBeNull()
    await choose('Storage profile', /s3-main/)
    fireEvent.click(await screen.findByRole('radio', { name: /Archive it to storage/ }))
    await choose('Local copies on servers', 'Keep for 1 day')
    fireEvent.click(screen.getByRole('radio', { name: /Sandbox name and ID/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))

    await waitFor(() => expect(calls.find((call) => call.method === 'PATCH')).toBeDefined())
    expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({
      storage_profile: 's3-main',
      stop_policy: 'archive',
      archive_local_retention: '1d',
      archive_snapshot_name: 'name',
    })
    expect(await screen.findByText(/every node needs/)).toBeInTheDocument()
  })

  it('clears the DNS profile with none', async () => {
    const calls = stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/clusters/eu-west': () => json(200, cluster),
      'PATCH /v0/clusters/eu-west': () => json(200, cluster),
      ...profiles,
    })
    open()
    fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))
    await choose('DNS profile', 'None')
    fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))
    await waitFor(() => expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ dns_profile: 'none' }))
  })

  it('marks outdated nodes with their reasons and the command to run', async () => {
    stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/clusters/eu-west': () => json(200, cluster),
    })
    open()
    const row = (await screen.findByText('node-1')).closest('tr')!
    expect(within(row).getByText('Outdated')).toBeInTheDocument()
    expect(within(row).getByText(/storage profile s3-main is at version 2/)).toBeInTheDocument()
    expect(within(row).getByText('lewbox node configure')).toBeInTheDocument()
    const other = screen.getByText('node-2').closest('tr')!
    expect(within(other).queryByText('Outdated')).toBeNull()
  })
})
