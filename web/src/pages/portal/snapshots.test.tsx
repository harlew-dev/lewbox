import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { acme, acmeMembership, customerUser, json, renderApp, stubAPI } from '../../test/render'

const snapshot = {
  name: 'ckpt-1',
  region: 'eu-west',
  source_sandbox_id: 'sb-0001',
  image: 'ubuntu:22.04',
  created_at: '2026-11-12T10:00:00Z',
  cpu: 1,
  memory_mb: 1024,
  disk_gb: 10,
  push_state: 'active',
}

describe('SnapshotsPage', () => {
  it('lists snapshots and deletes one after confirmation', async () => {
    let deleted = false
    const calls = stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/snapshots': () => json(200, { data: deleted ? [] : [snapshot], partial_nodes: [] }),
        'DELETE /v0/snapshots/ckpt-1': () => {
          deleted = true
          return json(200, snapshot)
        },
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/snapshots')

    const row = (await screen.findByText('ckpt-1')).closest('tr')!
    expect(within(row).getByText('eu-west')).toBeInTheDocument()
    expect(within(row).getByText('sb-0001')).toBeInTheDocument()
    expect(within(row).getByText('1 vCPU · 1 GB · 10 GB disk')).toBeInTheDocument()
    expect(within(row).getByText('Active')).toBeInTheDocument()
    expect(calls.find((call) => call.url.startsWith('/v0/snapshots'))?.org).toBe('acme')

    fireEvent.click(within(row).getByRole('button', { name: 'Delete ckpt-1' }))
    expect(calls.some((call) => call.method === 'DELETE')).toBe(false)
    fireEvent.click(await screen.findByRole('button', { name: 'Delete snapshot' }))

    await waitFor(() =>
      expect(calls.find((call) => call.method === 'DELETE')).toMatchObject({
        url: '/v0/snapshots/ckpt-1?region=eu-west',
        org: 'acme',
      }),
    )
    expect(await screen.findByText('Snapshots you take with the SDK show up here.')).toBeInTheDocument()
  })

  it('says where snapshots come from when there are none', async () => {
    stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/snapshots': () => json(200, { data: [], partial_nodes: [] }),
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/snapshots')
    expect(await screen.findByText('Snapshots you take with the SDK show up here.')).toBeInTheDocument()
  })
})
