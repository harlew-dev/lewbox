import { screen, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { choose, json, renderApp, stubAPI } from '../../test/render'

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

    await waitFor(() =>
      expect(screen.getByRole('combobox', { name: 'Region' })).toHaveTextContent('EU West'),
    )
    await waitFor(() => {
      const requests = calls.filter((call) => call.url.startsWith('/v0/sandboxes'))
      expect(requests.at(-1)?.url).toBe('/v0/sandboxes?region=eu-west&status=stopped')
    })

    await choose('Status', 'Running')
    await waitFor(() => {
      const requests = calls.filter((call) => call.url.startsWith('/v0/sandboxes'))
      expect(requests.at(-1)?.url).toBe('/v0/sandboxes?region=eu-west&status=started')
    })
  })
})
