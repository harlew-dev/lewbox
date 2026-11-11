import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { json, renderApp, stubAPI } from '../../test/render'

const settings = {
  instance_name: 'Lewbox',
  signup: 'closed',
  default_plan: 'free',
  currency: 'USD',
  price_vcpu_hour_micros: 50000,
  price_memory_gib_hour_micros: 16000,
  billing: 'off',
  signup_credits_micros: 0,
}

describe('AdminSettingsPage', () => {
  it('saves only what changed', async () => {
    const calls = stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/plans': () => json(200, { data: [] }),
      'GET /v0/settings': () => json(200, settings),
      'PATCH /v0/settings': (call) => json(200, { ...settings, ...(call.body as object) }),
    })
    renderApp('/admin/settings')

    const name = await screen.findByLabelText('Instance name')
    expect(screen.getByRole('button', { name: 'Save settings' })).toBeDisabled()
    fireEvent.change(name, { target: { value: 'Acme Cloud' } })
    fireEvent.click(screen.getByRole('radio', { name: /Approval/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Save settings' }))

    await waitFor(() =>
      expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({
        instance_name: 'Acme Cloud',
        signup: 'approval',
      }),
    )
  })
})
