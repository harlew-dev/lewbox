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

describe('Snapshots tab', () => {
  it('lists snapshots with region names and deletes one after confirmation', async () => {
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
    renderApp('/sandboxes?tab=snapshots')

    const tabs = await screen.findByRole('navigation', { name: 'Sandbox views' })
    expect(within(tabs).getByRole('link', { name: 'Snapshots' })).toHaveAttribute('aria-current', 'page')
    const row = (await screen.findByText('ckpt-1')).closest('tr')!
    expect(screen.queryByRole('columnheader', { name: 'Region' })).toBeNull()
    expect(within(row).getByText('sb-0001')).toBeInTheDocument()
    expect(within(row).getByText('1 vCPU · 1 GB · 10 GB disk')).toBeInTheDocument()
    expect(screen.queryByRole('columnheader', { name: 'Push state' })).toBeNull()
    expect(within(row).queryByText('Upload failed')).toBeNull()
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
    expect(await screen.findByText('Snapshots you take show up here.')).toBeInTheDocument()
  })

  it('marks a snapshot whose upload failed', async () => {
    stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/snapshots': () => json(200, { data: [{ ...snapshot, push_state: 'failed' }], partial_nodes: [] }),
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/sandboxes?tab=snapshots')
    const row = (await screen.findByText('ckpt-1')).closest('tr')!
    expect(within(row).getByText('Upload failed')).toBeInTheDocument()
  })

  it('shows a deleted snapshot again if a later load still has it', async () => {
    const calls = stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        // The region still lists it: the delete hasn't reached every node.
        'GET /v0/snapshots': () => json(200, { data: [snapshot], partial_nodes: [] }),
        'DELETE /v0/snapshots/ckpt-1': () => json(200, snapshot),
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/sandboxes?tab=snapshots')

    fireEvent.click(await screen.findByRole('button', { name: 'Delete ckpt-1' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete snapshot' }))
    await waitFor(() => expect(calls.filter((call) => call.url.startsWith('/v0/snapshots') && call.method === 'GET')).toHaveLength(2))
    expect(await screen.findByText('ckpt-1')).toBeInTheDocument()
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
    renderApp('/sandboxes?tab=snapshots')
    expect(await screen.findByText('Snapshots you take show up here.')).toBeInTheDocument()
  })
})
