import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { json, renderApp, stubAPI } from '../../test/render'

const day = (date: string, uptime: number | null) => ({ date, uptime, samples: 1440, down_minutes: 0 })

describe('OverviewPage', () => {
  it('shows the fleet in numbers, uptime per cluster and the top workspaces', async () => {
    stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/overview': () =>
        json(200, {
          customers: { total: 12, pending: 1, suspended: 0 },
          sandboxes: { running: 7, stopped: 2 },
          capacity: { nodes_alive: 2, nodes: 2, cpu_cores: 8, cpu_reserved: 7, memory_total_mb: 30728, memory_used_mb: 12000 },
          clusters: [
            { slug: 'eu-west', name: 'EU West', status: 'ready', uptime: 0.9995, days: [day('2026-11-10', 1), day('2026-11-11', 0.999)] },
          ],
          running: [],
          top_customers: [{ slug: 'acme', name: 'Acme', vcpu_hours: 120.5 }],
        }),
    })
    renderApp('/admin')

    expect(await screen.findByRole('heading', { name: 'Overview' })).toBeInTheDocument()
    expect(screen.getByText('7 / 8')).toBeInTheDocument()
    expect(screen.getByText('1 / 1')).toBeInTheDocument()
    expect(screen.getByRole('list', { name: /EU West, last 2 days/ })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'EU West' })).toHaveAttribute('href', '/admin/servers/clusters/eu-west')
    expect(screen.getByRole('link', { name: /Acme/ })).toHaveAttribute('href', '/admin/workspaces/acme')
  })
})
