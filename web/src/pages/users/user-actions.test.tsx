import { fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { adminUser, json, renderApp, stubAPI, type Call } from '../../test/render'

const target = {
  ...adminUser,
  id: '01a1cb34-659b-74a8-983d-085c0383e0b7',
  email: 'dev@example.com',
  instance_admin: false,
}

function stubUsers(routes: Record<string, (call: Call) => Response> = {}) {
  return stubAPI({
    'GET /v0/users': () => json(200, { data: [target] }),
    ...routes,
  })
}

async function openSetPassword() {
  renderApp('/admin/users')
  expect(await screen.findByText(target.email)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: `Actions for ${target.email}` }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Set password' }))
  expect(await screen.findByRole('heading', { name: 'Set a new password' })).toBeInTheDocument()
}

function fillNewPassword() {
  fireEvent.change(screen.getByLabelText('New password'), { target: { value: 'new-password-123' } })
  fireEvent.change(screen.getByLabelText('Confirm new password'), {
    target: { value: 'new-password-123' },
  })
}

function submit() {
  fireEvent.click(screen.getAllByRole('button', { name: 'Set password' }).at(-1)!)
}

describe('UserActions set password', () => {
  it('requires the admin’s own current password', async () => {
    const calls = stubUsers()
    await openSetPassword()
    fillNewPassword()
    submit()

    expect(await screen.findByText('Enter your password.')).toBeInTheDocument()
    expect(calls.some((call) => call.method === 'PATCH')).toBe(false)
  })

  it('shows a wrong current password on the field', async () => {
    stubUsers({
      [`PATCH /v0/users/${target.id}/password`]: () =>
        json(400, {
          error: {
            code: 'invalid_request',
            message: 'current password is wrong',
            details: { field: 'current_password' },
          },
        }),
    })
    await openSetPassword()
    fireEvent.change(screen.getByLabelText('Your password'), {
      target: { value: 'wrong-password' },
    })
    fillNewPassword()
    submit()

    expect(await screen.findByText('current password is wrong')).toBeInTheDocument()
    expect(screen.getByText("To confirm it's you.")).toBeInTheDocument()
  })

  it('sets the password with the admin’s current password', async () => {
    const calls = stubUsers({
      [`PATCH /v0/users/${target.id}/password`]: () => json(200, target),
    })
    await openSetPassword()
    fireEvent.change(screen.getByLabelText('Your password'), {
      target: { value: 'admin-password-1' },
    })
    fillNewPassword()
    submit()

    await waitFor(() =>
      expect(calls.find((call) => call.method === 'PATCH')).toMatchObject({
        url: `/v0/users/${target.id}/password`,
        body: { password: 'new-password-123', current_password: 'admin-password-1' },
      }),
    )
    await waitFor(() =>
      expect(screen.queryByRole('heading', { name: 'Set a new password' })).not.toBeInTheDocument(),
    )
  })
})
