import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { json, renderApp, stubAPI } from '../../test/render'

const euWest = {
  slug: 'eu-west',
  name: 'EU West',
  endpoint_url: 'https://eu.example',
  runtime: 'docker',
  status: 'ready',
}

describe('FleetSandboxesPage', () => {
  it('restores the region and status filters from the URL', async () => {
    const calls = stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/regions': () => json(200, { data: [euWest] }),
      'GET /v0/sandboxes': () => json(200, { data: [] }),
    })
    renderApp('/admin/sandboxes?region=eu-west&status=stopped')

    await waitFor(() => {
      const requests = calls.filter((call) => call.url.startsWith('/v0/sandboxes'))
      expect(requests.at(-1)?.url).toBe('/v0/sandboxes?status=stopped&region=eu-west')
    })
    expect(await screen.findByRole('button', { name: /Region.*EU West/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Status.*Stopped/ })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Remove Region filter' }))
    await waitFor(() => {
      const requests = calls.filter((call) => call.url.startsWith('/v0/sandboxes'))
      expect(requests.at(-1)?.url).toBe('/v0/sandboxes?status=stopped')
    })
  })

  it('shows each sandbox node, runtime and size', async () => {
    stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/regions': () => json(200, { data: [euWest] }),
      'GET /v0/sandboxes': () =>
        json(200, {
          data: [
            {
              id: 'sb-7f3a9c',
              region: 'eu-west',
              organization: { slug: 'acme', name: 'Acme' },
              name: '',
              image: 'ubuntu:22.04',
              status: 'started',
              status_reason: '',
              runtime: 'firecracker',
              cpu: 2,
              memory_mb: 2048,
              disk_gb: 20,
              node: 'node-a',
              created_at: '2026-11-11T15:30:00Z',
              last_seen_at: '2026-11-11T15:40:00Z',
            },
          ],
        }),
    })
    renderApp('/admin/sandboxes')

    expect(await screen.findByText('firecracker')).toBeInTheDocument()
    expect(screen.getByText('node-a')).toBeInTheDocument()
    expect(screen.getByText('2 vCPU · 2 GB · 20 GB disk')).toBeInTheDocument()
  })
})
