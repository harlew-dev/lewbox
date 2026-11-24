import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { describe, expect, it } from 'vitest'
import { AppLinkProvider } from '../components/app-link'
import { AuthProvider } from '../lib/auth'
import { json, stubAPI, type Call } from '../test/render'
import { AccountPage } from './account'

const user = {
  id: '01a1cb34-659b-74a8-983d-085c0383e0a6',
  email: 'admin@example.com',
  instance_admin: true,
  disabled: false,
  created_at: '2026-11-11T15:22:51.1Z',
}

const token = 'lbp_' + 'b'.repeat(58) + 'AbCdEf'

const record = {
  id: '01a1cb34-29a5-74c3-9691-5394d021b0fb',
  user_id: user.id,
  name: 'ci',
  prefix: token.slice(0, 12),
  last4: token.slice(-4),
  status: 'active',
  created_at: '2026-11-13T10:00:00Z',
}

function stubAccount(routes: Record<string, (call: Call) => Response> = {}) {
  return stubAPI(
    {
      'GET /v0/access-tokens': () => json(200, { data: [] }),
      ...routes,
    },
    user,
  )
}

function renderAccount() {
  render(
    <MemoryRouter initialEntries={['/account']}>
      <AppLinkProvider>
        <AuthProvider>
          <AccountPage />
        </AuthProvider>
      </AppLinkProvider>
    </MemoryRouter>,
  )
}

describe('AccountPage', () => {
  it('shows the account and the password form, and leaves signing out to the account menu', async () => {
    stubAccount()
    renderAccount()

    expect(await screen.findByRole('heading', { name: 'Account settings' })).toBeInTheDocument()
    expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Change password' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /sign out/i })).not.toBeInTheDocument()
    expect(screen.queryByText(/sign out/i, { selector: 'h2,h3' })).not.toBeInTheDocument()
  })

  it('creates an access token shown once, with the CLI sign-in', async () => {
    const calls = stubAccount({
      'POST /v0/access-tokens': (call) => json(201, { ...record, name: (call.body as { name: string }).name, token }),
    })
    renderAccount()

    expect(await screen.findByText('No access tokens')).toBeInTheDocument()
    expect(screen.getByText(/including clusters and nodes/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Create token' }))
    expect(await screen.findByRole('heading', { name: 'Create access token' })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'deploy' } })
    fireEvent.change(screen.getAllByLabelText('Current password').at(-1)!, {
      target: { value: 'admin-password-1' },
    })
    fireEvent.click(screen.getAllByRole('button', { name: 'Create token' }).at(-1)!)

    expect(await screen.findByText(token)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copy access token' })).toBeInTheDocument()
    expect(screen.getByText(/lewbox login .* --token/)).toBeInTheDocument()
    const create = calls.find((call) => call.method === 'POST' && call.url === '/v0/access-tokens')
    expect(create?.org).toBeNull()
    expect(create?.body).toEqual({
      name: 'deploy',
      expires_in: '90d',
      current_password: 'admin-password-1',
    })
  })

  it('requires the current password to create an access token', async () => {
    const calls = stubAccount()
    renderAccount()

    expect(await screen.findByText('No access tokens')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Create token' }))
    expect(await screen.findByRole('heading', { name: 'Create access token' })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'deploy' } })
    fireEvent.click(screen.getAllByRole('button', { name: 'Create token' }).at(-1)!)

    expect(await screen.findByText('Enter your current password.')).toBeInTheDocument()
    expect(calls.some((call) => call.method === 'POST')).toBe(false)
  })

  it('shows a wrong current password on the field when creating an access token', async () => {
    stubAccount({
      'POST /v0/access-tokens': () =>
        json(400, {
          error: {
            code: 'invalid_request',
            message: 'current password is wrong',
            details: { field: 'current_password' },
          },
        }),
    })
    renderAccount()

    expect(await screen.findByText('No access tokens')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Create token' }))
    expect(await screen.findByRole('heading', { name: 'Create access token' })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'deploy' } })
    fireEvent.change(screen.getAllByLabelText('Current password').at(-1)!, {
      target: { value: 'wrong-password' },
    })
    fireEvent.click(screen.getAllByRole('button', { name: 'Create token' }).at(-1)!)

    expect(await screen.findByText('current password is wrong')).toBeInTheDocument()
  })

  it('changes the password with the current password and clears the fields', async () => {
    const calls = stubAccount({
      [`PATCH /v0/users/${user.id}/password`]: () => json(200, user),
    })
    renderAccount()

    fireEvent.change(await screen.findByLabelText('Current password'), {
      target: { value: 'admin-password-1' },
    })
    fireEvent.change(screen.getByLabelText('New password'), { target: { value: 'new-password-123' } })
    fireEvent.change(screen.getByLabelText('Confirm new password'), {
      target: { value: 'new-password-123' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Change password' }))

    await waitFor(() =>
      expect(calls.find((call) => call.method === 'PATCH')).toMatchObject({
        url: `/v0/users/${user.id}/password`,
        body: { password: 'new-password-123', current_password: 'admin-password-1' },
      }),
    )
    await waitFor(() => expect(screen.getByLabelText('Current password')).toHaveValue(''))
    expect(screen.getByLabelText('New password')).toHaveValue('')
    expect(screen.getByLabelText('Confirm new password')).toHaveValue('')
  })

  it('requires the current password to change the password', async () => {
    const calls = stubAccount()
    renderAccount()

    fireEvent.change(await screen.findByLabelText('New password'), { target: { value: 'new-password-123' } })
    fireEvent.change(screen.getByLabelText('Confirm new password'), {
      target: { value: 'new-password-123' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Change password' }))

    expect(await screen.findByText('Enter your current password.')).toBeInTheDocument()
    expect(calls.some((call) => call.method === 'PATCH')).toBe(false)
  })

  it('shows a wrong current password on the field when changing the password', async () => {
    stubAccount({
      [`PATCH /v0/users/${user.id}/password`]: () =>
        json(400, {
          error: {
            code: 'invalid_request',
            message: 'current password is wrong',
            details: { field: 'current_password' },
          },
        }),
    })
    renderAccount()

    fireEvent.change(await screen.findByLabelText('Current password'), {
      target: { value: 'wrong-password' },
    })
    fireEvent.change(screen.getByLabelText('New password'), { target: { value: 'new-password-123' } })
    fireEvent.change(screen.getByLabelText('Confirm new password'), {
      target: { value: 'new-password-123' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Change password' }))

    expect(await screen.findByText('current password is wrong')).toBeInTheDocument()
  })

  it('lists tokens and revokes one', async () => {
    const calls = stubAccount({
      'GET /v0/access-tokens': () => json(200, { data: [record] }),
      [`DELETE /v0/access-tokens/${record.id}`]: () => json(200, { ...record, status: 'revoked' }),
    })
    renderAccount()

    expect(await screen.findByText('ci')).toBeInTheDocument()
    expect(screen.getByText(`${record.prefix}…${record.last4}`)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Actions for ci' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Revoke token' }))
    fireEvent.click((await screen.findAllByRole('button', { name: 'Revoke token' })).at(-1)!)

    // A successful revoke reloads the list.
    await waitFor(() => expect(calls.filter((call) => call.url === '/v0/access-tokens')).toHaveLength(2))
    expect(calls.filter((call) => call.method === 'DELETE')).toHaveLength(1)
  })
})
