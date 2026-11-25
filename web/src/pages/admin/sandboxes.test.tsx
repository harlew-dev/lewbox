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
})
