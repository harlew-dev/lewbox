import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { json, renderApp, stubAPI } from '../../test/render'

const event = {
  id: '01a1cb34-29a5-74c3-9691-5394d021b111',
  name: 'workspace.updated',
  actor_kind: 'user',
  actor_id: '01a1cb34-29a5-74c3-9691-5394d021b222',
  actor_label: 'admin@example.com',
  target_type: 'workspace',
  target_id: '01a1cb34-29a5-74c3-9691-5394d021b333',
  target_name: 'acme',
  organization_id: '01a1cb34-29a5-74c3-9691-5394d021b333',
  organization_slug: 'acme',
  changes: { name: { from: 'Acme', to: 'Acme Ltd' } },
  ip: '203.0.113.9',
  request_id: 'req_abcdefghijklmn',
  created_at: '2026-11-11T15:22:35.8Z',
}

describe('EventsPage', () => {
  it('links the workspace slug and folds the IP into the details', async () => {
    stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/events': () =>
        json(200, { data: [event], next: null }),
    })
    renderApp('/admin/events')

    expect(await screen.findByText('workspace.updated')).toBeInTheDocument()
    const workspace = screen.getByRole('link', { name: 'acme' })
    expect(workspace).toHaveAttribute('href', '/admin/workspaces/acme')
    expect(screen.queryByText('203.0.113.9')).not.toBeInTheDocument()

    fireEvent.click(
      screen.getByRole('button', { name: 'Details for workspace.updated' }),
    )
    expect(screen.getByText('203.0.113.9')).toBeInTheDocument()
    expect(screen.getByText('req_abcdefghijklmn')).toBeInTheDocument()
  })
})
