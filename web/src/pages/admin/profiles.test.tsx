import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { choose, json, renderApp, stubAPI } from '../../test/render'

const dns = {
  id: 'd1',
  name: 'cf-prod',
  provider: 'cloudflare',
  zone: 'example.com',
  zone_id: 'z1',
  token_last4: 'abcd',
  acme_email: 'ops@example.com',
  version: 1,
  clusters: 2,
  tested_at: '2026-11-12T10:00:00Z',
  test_error: '',
  created_at: '',
  updated_at: '',
}

const storage = {
  id: 's1',
  name: 's3-main',
  provider: 's3',
  endpoint: '',
  region: 'eu-west-1',
  bucket: 'lewbox',
  prefix: '',
  path_style: false,
  access_key_id: 'AKIA1',
  secret_last4: 'wxyz',
  version: 1,
  clusters: 0,
  tested_at: null,
  test_error: '',
  created_at: '',
  updated_at: '',
}

describe('DNS profiles tab', () => {
  it('lists profiles with the masked token and last test', async () => {
    stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/dns-profiles': () => json(200, { data: [{ ...dns, tested_at: '2026-11-12T10:00:00Z', test_error: 'zone not found' }] }),
    })
    renderApp('/admin/settings?tab=dns')
    const row = (await screen.findByText('cf-prod')).closest('tr')!
    expect(within(row).getByText('example.com')).toBeInTheDocument()
    expect(within(row).getByText('…abcd')).toBeInTheDocument()
    expect(within(row).getByText('zone not found')).toBeInTheDocument()
    expect(within(row).getByText('2')).toBeInTheDocument()
  })

  it('shows the failed step and keeps the dialog open when the test fails on save', async () => {
    const calls = stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/dns-profiles': () => json(200, { data: [] }),
      'POST /v0/dns-profiles': () =>
        json(412, { error: { code: 'precondition_failed', message: 'DNS profile test failed: no edit access', details: { step: 'write' } } }),
    })
    renderApp('/admin/settings?tab=dns')
    fireEvent.click((await screen.findAllByRole('button', { name: 'New DNS profile' }))[0]!)
    fireEvent.change(await screen.findByLabelText('Name'), { target: { value: 'cf-new' } })
    fireEvent.change(screen.getByLabelText('Zone'), { target: { value: 'example.org' } })
    fireEvent.change(screen.getByLabelText('API token'), { target: { value: 'secret-token' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create and test' }))

    expect(await screen.findByText(/no edit access \(step: write\)/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Create and test' })).toBeInTheDocument()
    expect(calls.find((call) => call.method === 'POST')?.body).toMatchObject({
      name: 'cf-new',
      zone: 'example.org',
      api_token: 'secret-token',
    })
  })

  it('does not send the token when an edit leaves it empty', async () => {
    const calls = stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/dns-profiles': () => json(200, { data: [dns] }),
      'PATCH /v0/dns-profiles/cf-prod': () => json(200, dns),
    })
    renderApp('/admin/settings?tab=dns')
    fireEvent.click(await screen.findByRole('button', { name: 'Edit cf-prod' }))
    fireEvent.change(await screen.findByLabelText('ACME email'), { target: { value: 'new@example.com' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save and test' }))
    await waitFor(() => expect(calls.find((call) => call.method === 'PATCH')).toBeDefined())
    expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ acme_email: 'new@example.com' })
  })

  it('tests a profile and shows the server’s message when clusters block a delete', async () => {
    const calls = stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/dns-profiles': () => json(200, { data: [dns] }),
      'POST /v0/dns-profiles/cf-prod/test': () => json(200, dns),
      'DELETE /v0/dns-profiles/cf-prod': () =>
        json(409, { error: { code: 'conflict', message: 'DNS profile cf-prod is used by 2 clusters', details: null } }),
    })
    renderApp('/admin/settings?tab=dns')
    fireEvent.click(await screen.findByRole('button', { name: 'Test cf-prod' }))
    await waitFor(() => expect(calls.some((call) => call.url === '/v0/dns-profiles/cf-prod/test')).toBe(true))
    fireEvent.click(screen.getByRole('button', { name: 'Delete cf-prod' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete profile' }))
    expect(await screen.findByText('DNS profile cf-prod is used by 2 clusters')).toBeInTheDocument()
  })
})

describe('Storage profiles tab', () => {
  it('fills the R2 endpoint, region and path style from the account ID', async () => {
    const calls = stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/storage-profiles': () => json(200, { data: [] }),
      'POST /v0/storage-profiles': () => json(200, storage),
    })
    renderApp('/admin/settings?tab=storage')
    fireEvent.click((await screen.findAllByRole('button', { name: 'New storage profile' }))[0]!)
    await choose('Provider', 'Cloudflare R2')
    fireEvent.change(await screen.findByLabelText('Account ID'), { target: { value: 'acct123' } })
    expect(screen.getByText('Endpoint: https://acct123.r2.cloudflarestorage.com')).toBeInTheDocument()
    expect(screen.getByLabelText('Region')).toHaveValue('auto')
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'r2' } })
    fireEvent.change(screen.getByLabelText('Bucket'), { target: { value: 'snaps' } })
    fireEvent.change(screen.getByLabelText('Access key ID'), { target: { value: 'AK' } })
    fireEvent.change(screen.getByLabelText('Secret access key'), { target: { value: 'SK' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create and test' }))

    await waitFor(() => expect(calls.find((call) => call.method === 'POST')).toBeDefined())
    expect(calls.find((call) => call.method === 'POST')?.body).toMatchObject({
      endpoint: 'https://acct123.r2.cloudflarestorage.com',
      region: 'auto',
      path_style: true,
      bucket: 'snaps',
      secret_access_key: 'SK',
    })
  })

  it('shows the failed step, and an edit with an empty secret does not send it', async () => {
    let fail = true
    const calls = stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/storage-profiles': () => json(200, { data: [storage] }),
      'PATCH /v0/storage-profiles/s3-main': () =>
        fail
          ? json(412, { error: { code: 'precondition_failed', message: 'storage profile test failed: access denied', details: { step: 'list' } } })
          : json(200, storage),
    })
    renderApp('/admin/settings?tab=storage')
    const row = (await screen.findByText('s3-main')).closest('tr')!
    expect(within(row).getByText('…wxyz')).toBeInTheDocument()
    expect(within(row).getByText('Never')).toBeInTheDocument()
    fireEvent.click(within(row).getByRole('button', { name: 'Edit s3-main' }))
    fireEvent.change(await screen.findByLabelText('Bucket'), { target: { value: 'other' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save and test' }))
    expect(await screen.findByText(/access denied \(step: list\)/)).toBeInTheDocument()
    fail = false
    fireEvent.click(screen.getByRole('button', { name: 'Save and test' }))
    await waitFor(() => expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(2))
    for (const call of calls.filter((item) => item.method === 'PATCH')) {
      expect(call.body).toEqual({ bucket: 'other' })
    }
  })
})
