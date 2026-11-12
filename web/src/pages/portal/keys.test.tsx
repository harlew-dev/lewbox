import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { acme, acmeMembership, customerUser, json, renderApp, stubAPI } from '../../test/render'

const region = {
  slug: 'eu-west',
  name: 'EU West',
  endpoint_url: 'https://eu-west.api.acme.dev',
  runtime: 'docker',
  status: 'ready',
}

const token = 'avm_' + 'x'.repeat(40) + 'k3Xz'

describe('KeysPage', () => {
  it('creates a key, shows it once with the snippets, then only its hint', async () => {
    let created = false
    const key = {
      id: '01a1cb34-29a5-74c3-9691-5394d021b0fb',
      name: 'laptop',
      region,
      token_hint: 'avm_…k3Xz',
      created_by: customerUser.email,
      created_at: '2026-11-11T15:30:00Z',
      expires_at: null,
      revoked_at: null,
      expired: false,
    }
    const calls = stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/regions': () => json(200, { data: [region] }),
        'GET /v0/sandbox-keys': () => json(200, { data: created ? [key] : [] }),
        'POST /v0/sandbox-keys': () => {
          created = true
          return json(201, {
            ...key,
            token,
            env: { SB_API_URL: region.endpoint_url, SB_API_TOKEN: token },
            e2b_env: {
              E2B_API_URL: `${region.endpoint_url}/e2b`,
              E2B_SANDBOX_URL: `${region.endpoint_url}/e2b/runtime`,
              E2B_API_KEY: token,
            },
          })
        },
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/keys')

    fireEvent.click(await screen.findByRole('button', { name: 'Create API key' }))
    fireEvent.change(await screen.findByLabelText('Name'), { target: { value: 'laptop' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create key' }))

    expect(await screen.findByText(token)).toBeInTheDocument()
    expect(screen.getByText('Copy it now. You won’t see it again.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copy API key' })).toBeInTheDocument()
    expect(screen.getByText(`export SB_API_URL=${region.endpoint_url}\nexport SB_API_TOKEN=${token}`, { normalizer: (s) => s })).toBeInTheDocument()
    for (const name of ['TypeScript', 'Python', 'E2B', 'curl']) {
      expect(screen.getByRole('tab', { name })).toBeInTheDocument()
    }
    expect(screen.getByText(/@aerol-ai\/aerolvm-sdk/)).toBeInTheDocument()
    expect(screen.getByText('At a limit, creating a sandbox returns 429.')).toBeInTheDocument()

    const create = calls.find((call) => call.method === 'POST' && call.url === '/v0/sandbox-keys')
    expect(create?.org).toBe('acme')
    expect(create?.body).toEqual({ name: 'laptop', region: 'eu-west', expires_in: '90d' })

    fireEvent.click(screen.getByRole('button', { name: 'Done' }))
    await waitFor(() => expect(screen.queryByText(token)).toBeNull())
    expect(await screen.findByText('avm_…k3Xz')).toBeInTheDocument()
  })

  it('offers only the regions the workspace has a plan in', async () => {
    const usEast = { ...region, slug: 'us-east', name: 'US East' }
    const apSouth = { ...region, slug: 'ap-south', name: 'AP South' }
    const org = {
      ...acme,
      regions: [
        { slug: 'eu-west', name: 'EU West', plan: { slug: 'free', name: 'Free' } },
        { slug: 'us-east', name: 'US East', plan: { slug: 'pro', name: 'Pro' } },
      ],
    }
    const calls = stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, org),
        // An instance admin who is a member is sent every region.
        'GET /v0/regions': () => json(200, { data: [region, usEast, apSouth] }),
        'GET /v0/sandbox-keys': () => json(200, { data: [] }),
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/keys')

    fireEvent.click(await screen.findByRole('button', { name: 'Create API key' }))
    const picker = await screen.findByRole('combobox', { name: 'Region' })
    expect(calls.find((call) => call.url === '/v0/regions')?.org).toBe('acme')
    fireEvent.click(picker)
    const options = await screen.findAllByRole('option')
    expect(options.map((option) => option.textContent)).toEqual(['EU West', 'US East'])
    expect(within(document.body).queryByRole('option', { name: 'AP South' })).toBeNull()
  })

  it('disables creating a key until the workspace has a region', async () => {
    stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, { ...acme, regions: [] }),
        'GET /v0/sandbox-keys': () => json(200, { data: [] }),
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/keys')

    expect(await screen.findByRole('button', { name: /Create API key/ })).toBeDisabled()
    expect(screen.getAllByText('Your workspace doesn’t have a region yet. Your provider will enable one.').length).toBeGreaterThan(0)
  })
})

describe('key expiry', () => {
  it('sends "never" for a sandbox key that never expires', async () => {
    const calls = stubAPI(
      {
        'GET /v0/organizations/acme': () => json(200, acme),
        'GET /v0/regions': () => json(200, { data: [region] }),
        'GET /v0/sandbox-keys': () => json(200, { data: [] }),
        'POST /v0/sandbox-keys': () => json(400, { error: { code: 'invalid', message: 'stop here' } }),
      },
      customerUser,
      [acmeMembership],
    )
    renderApp('/keys')

    fireEvent.click(await screen.findByRole('button', { name: 'Create API key' }))
    fireEvent.change(await screen.findByLabelText('Name'), { target: { value: 'ci' } })
    fireEvent.click(screen.getByRole('combobox', { name: 'Expires' }))
    expect((await screen.findAllByRole('option')).map((option) => option.textContent)).toEqual([
      'In 30 days',
      'In 90 days',
      'In a year',
      'Never',
    ])
    // Base UI commits an option only after a press on it, as a mouse does.
    fireEvent.pointerDown(screen.getByRole('option', { name: 'Never' }))
    fireEvent.click(screen.getByRole('option', { name: 'Never' }))
    await waitFor(() => expect(screen.getByRole('combobox', { name: 'Expires' })).toHaveTextContent('Never'))
    fireEvent.click(screen.getByRole('button', { name: 'Create key' }))

    expect(await screen.findByText('stop here')).toBeInTheDocument()
    expect(calls.find((call) => call.method === 'POST')?.body).toEqual({ name: 'ci', region: 'eu-west', expires_in: 'never' })
  })
})
