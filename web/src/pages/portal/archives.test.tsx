import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { acme, acmeMembership, customerUser, json, renderApp, stubAPI } from '../../test/render'

const archive = {
  id: '01a1cb34-29a5-74c3-9691-5394d021b0aa',
  kind: 'archive',
  name: 'archive-1',
  region: 'eu-west',
  sandbox_name: 'builder',
  source_sandbox_id: 'sb-0001',
  image: 'ubuntu:22.04',
  runtime: 'docker',
  created_at: '2026-11-12T10:00:00Z',
  archived_at: '2026-11-12T10:05:00Z',
  cpu: 2,
  memory_mb: 2048,
  disk_gb: 20,
  tags: {},
  status: 'active',
  error: '',
  registry_ref: '',
  last_restored_sandbox_id: 'sb-0009',
  last_restored_at: '2026-11-12T11:00:00Z',
}

const region = (stop_policy: string) => ({
  slug: 'eu-west', name: 'EU West', endpoint_url: 'https://eu.example', runtime: 'docker', status: 'ready', stop_policy,
})

function stub(list: unknown[], stop_policy = 'archive', member = acmeMembership) {
  return stubAPI(
    {
      'GET /v0/organizations/acme': () => json(200, member.role === 'viewer' ? { ...acme, role: 'viewer' } : acme),
      'GET /v0/regions': () => json(200, { data: [region(stop_policy)] }),
      'GET /v0/snapshots': () => json(200, { data: list, partial_nodes: [] }),
      'GET /v0/sandboxes': () => json(200, { data: [] }),
      [`POST /v0/archives/${archive.id}/restore`]: () =>
        json(200, { archive, sandbox: { id: 'sb-new-1', name: 'builder', status: 'starting', region: 'eu-west' } }),
      [`DELETE /v0/archives/${archive.id}`]: () => json(200, archive),
    },
    customerUser,
    [member],
  )
}

describe('Archived sandboxes in the Snapshots tab', () => {
  it('lists archives and restores one as a new sandbox', async () => {
    const calls = stub([archive])
    renderApp('/sandboxes?tab=snapshots')
    const row = (await screen.findByText('builder')).closest('tr')!
    expect(within(row).getByText('Archived sandbox')).toBeInTheDocument()
    expect(within(row).getByText('Active')).toBeInTheDocument()
    expect(within(row).getByText('2 vCPU · 2 GB · 20 GB disk')).toBeInTheDocument()
    expect(within(row).getByText('sb-0009')).toBeInTheDocument()

    fireEvent.click(within(row).getByRole('button', { name: 'Restore builder' }))
    expect(await screen.findByText(/new sandbox with a new ID/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Restore sandbox' }))
    expect(await screen.findByText('sb-new-1')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copy sb-new-1' })).toBeInTheDocument()
    expect(calls.find((call) => call.method === 'POST')).toMatchObject({ org: 'acme' })
  })

  it('deletes an archive after confirmation', async () => {
    let gone = false
    const calls = stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/regions': () => json(200, { data: [region('archive')] }),
        'GET /v0/snapshots': () => json(200, { data: gone ? [] : [archive], partial_nodes: [] }),
        [`DELETE /v0/archives/${archive.id}`]: () => {
          gone = true
          return json(200, archive)
        },
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/sandboxes?tab=snapshots')
    fireEvent.click(await screen.findByRole('button', { name: 'Delete builder' }))
    expect(calls.some((call) => call.method === 'DELETE')).toBe(false)
    fireEvent.click(await screen.findByRole('button', { name: 'Delete archive' }))
    await waitFor(() => expect(calls.find((call) => call.method === 'DELETE')).toMatchObject({ org: 'acme' }))
    expect(await screen.findByText('No snapshots')).toBeInTheDocument()
  })

  it('shows archiving and failed archives, and no actions for a viewer', async () => {
    stub(
      [
        { ...archive, id: 'a2', name: 'archive-2', sandbox_name: 'slow', status: 'pushing', archived_at: null },
        { ...archive, id: 'a3', name: 'archive-3', sandbox_name: 'broken', status: 'error', error: 'bucket unreachable' },
      ],
      'archive',
      { ...acmeMembership, role: 'viewer' },
    )
    renderApp('/sandboxes?tab=snapshots')
    const slow = (await screen.findByText('slow')).closest('tr')!
    expect(within(slow).getByText('Pushing')).toBeInTheDocument()
    expect(screen.getByText('bucket unreachable')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^(Restore|Delete) / })).toBeNull()
  })

  it('explains an empty merged tab', async () => {
    stub([])
    renderApp('/sandboxes?tab=snapshots')
    expect(await screen.findByText('No snapshots')).toBeInTheDocument()
    expect(screen.getByText(/archived on stop show up here/)).toBeInTheDocument()
  })

  it('shows the note on the Sandboxes tab of an archive region, and no Archived tab', async () => {
    stub([])
    renderApp('/sandboxes')
    expect(
      await screen.findByText(/Stopped sandboxes in EU West are archived to storage and come back as new sandboxes/),
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'How archiving works' })).toHaveAttribute('href', '/docs/sandboxes/archived-sandboxes')
    expect(screen.getByRole('link', { name: 'Snapshots' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Archived' })).toBeNull()
  })

  it('never shows an Archived tab on a keep region', async () => {
    stub([], 'keep')
    renderApp('/sandboxes')
    await screen.findByRole('link', { name: 'Snapshots' })
    expect(screen.queryByRole('link', { name: 'Archived' })).toBeNull()
  })
})
