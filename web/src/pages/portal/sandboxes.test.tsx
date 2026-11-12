import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { acme, acmeMembership, customerUser, json, renderApp, stubAPI } from '../../test/render'

const sandbox = {
  id: 'sb-7f3a9c',
  region: 'eu-west',
  name: '',
  image: 'ubuntu:22.04',
  status: 'started',
  status_reason: '',
  runtime: 'docker',
  cpu: 1,
  memory_mb: 1024,
  disk_gb: 10,
  created_at: '2026-11-11T15:30:00Z',
  last_seen_at: '2026-11-11T15:40:00Z',
}

describe('SandboxesPage', () => {
  it('stops a sandbox once it is confirmed', async () => {
    const calls = stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/regions': () =>
          json(200, { data: [{ slug: 'eu-west', name: 'EU West', endpoint_url: 'https://eu.example', runtime: 'docker' }] }),
        'GET /v0/sandboxes?org=acme': () => json(200, { data: [sandbox] }),
        'POST /v0/sandboxes/sb-7f3a9c/stop?region=eu-west': () => json(200, { ...sandbox, status: 'stopped' }),
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/sandboxes')

    expect(await screen.findByText('sb-7f3a9c')).toBeInTheDocument()
    expect(screen.getByText('Running')).toBeInTheDocument()
    expect(screen.getByText('1 vCPU · 1 GB')).toBeInTheDocument()
    // Customers read region names, never slugs.
    const row = screen.getByText('sb-7f3a9c').closest('tr')!
    await waitFor(() => expect(within(row).getByText('EU West')).toBeInTheDocument())
    expect(within(row).queryByText('eu-west')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Actions for sb-7f3a9c' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Stop' }))
    expect(await screen.findByRole('heading', { name: 'Stop sb-7f3a9c?' })).toBeInTheDocument()
    expect(screen.getByText('It still counts toward your limits until you delete it.')).toBeInTheDocument()
    expect(calls.some((call) => call.method === 'POST')).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: 'Stop sandbox' }))
    await waitFor(() =>
      expect(calls.find((call) => call.method === 'POST')).toMatchObject({
        url: '/v0/sandboxes/sb-7f3a9c/stop?region=eu-west',
        org: 'acme',
      }),
    )
  })
})
