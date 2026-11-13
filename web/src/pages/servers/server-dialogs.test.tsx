import { fireEvent, screen, waitFor } from '@testing-library/react'
import { Route } from 'react-router'
import { describe, expect, it } from 'vitest'
import { choose, json, renderAt, stubAPI } from '../../test/render'
import { AddNodeDialog } from './add-node-dialog'
import { StartClusterDialog } from './start-cluster-dialog'
import type { Cluster } from '../../lib/api'

const dnsProfile = {
  id: 'd1', name: 'cf-prod', provider: 'cloudflare', zone: 'example.com', zone_id: 'z', token_last4: 'abcd',
  acme_email: 'ops@example.com', version: 1, clusters: 0, tested_at: null, test_error: '', created_at: '', updated_at: '',
}
const storageProfile = {
  id: 's1', name: 's3-main', provider: 's3', endpoint: '', region: 'eu-west-1', bucket: 'lewbox', prefix: '',
  path_style: false, access_key_id: 'AK', secret_last4: 'wxyz', version: 1, clusters: 0, tested_at: null,
  test_error: '', created_at: '', updated_at: '',
}

function stub() {
  return stubAPI({
    'GET /v0/organizations': () => json(200, { data: [] }),
    'GET /v0/dns-profiles': () => json(200, { data: [dnsProfile] }),
    'GET /v0/storage-profiles': () => json(200, { data: [storageProfile] }),
  })
}

function text(label: string) {
  return screen.getByLabelText(label)
}

function code() {
  return [...document.querySelectorAll('pre, code')].map((node) => node.textContent).join('\n')
}

describe('StartClusterDialog', () => {
  it('builds domain, DNS profile, storage and archive commands in two steps', async () => {
    stub()
    renderAt('/', <Route path="/" element={<StartClusterDialog open onClose={() => {}} />} />)
    fireEvent.change(await screen.findByLabelText('Cluster slug'), { target: { value: 'eu-west' } })
    fireEvent.change(text('Domain'), { target: { value: 'eu.api.example.com' } })
    await choose('DNS profile', /cf-prod/)
    expect(text('ACME email')).toHaveAttribute('placeholder', 'ops@example.com')
    await choose('Storage profile', /s3-main/)
    fireEvent.click(screen.getByRole('radio', { name: /Archive it to storage/ }))
    expect(screen.getByRole('button', { name: /Next/ })).toBeEnabled()
    fireEvent.click(screen.getByRole('button', { name: /Next/ }))

    expect(await screen.findByText('Install AerolVM')).toBeInTheDocument()
    const commands = code()
    expect(commands).toContain(
      'lewbox install --domain eu.api.example.com --dns-profile cf-prod --runtime docker --name node-1',
    )
    expect(commands).toContain('lewbox node add node-1 --new-cluster eu-west --storage-profile s3-main --stop-policy archive')
    expect(commands).not.toContain('--dns-records off')

    // Back keeps the values.
    fireEvent.click(screen.getByRole('button', { name: 'Back' }))
    expect(await screen.findByLabelText('Cluster slug')).toHaveValue('eu-west')
    expect(text('Domain')).toHaveValue('eu.api.example.com')
  })

  it('offers the records-off command when Lewbox does not manage DNS', async () => {
    stub()
    renderAt('/', <Route path="/" element={<StartClusterDialog open onClose={() => {}} />} />)
    fireEvent.change(await screen.findByLabelText('Cluster slug'), { target: { value: 'eu-west' } })
    fireEvent.change(text('Domain'), { target: { value: 'eu.api.example.com' } })
    await choose('DNS profile', /cf-prod/)
    fireEvent.click(screen.getByRole('checkbox', { name: /Lewbox manages DNS records/ }))
    fireEvent.click(screen.getByRole('button', { name: /Next/ }))
    await screen.findByText('Install AerolVM')
    expect(code()).toContain('lewbox cluster set eu-west --dns-records off')
  })

  it('builds Public IP commands and disables archiving without storage', async () => {
    stub()
    renderAt('/', <Route path="/" element={<StartClusterDialog open onClose={() => {}} />} />)
    fireEvent.change(await screen.findByLabelText('Cluster slug'), { target: { value: 'lab' } })
    fireEvent.click(screen.getByRole('radio', { name: /Public IP/ }))
    fireEvent.click(screen.getByRole('radio', { name: /Archive it to storage/ }))
    expect(screen.getByRole('radio', { name: /Archive it to storage/ })).toHaveAttribute('aria-checked', 'false')
    expect(screen.getByText(/Choose a storage profile first/, { selector: 'p' })).toBeInTheDocument()
    fireEvent.change(text('Public host'), { target: { value: '10.0.0.5' } })
    fireEvent.click(screen.getByText('Advanced'))
    fireEvent.change(await screen.findByLabelText('Extra installer flags'), { target: { value: '--lb-health-port=8081' } })
    fireEvent.click(screen.getByRole('button', { name: /Next/ }))
    await screen.findByText('Install AerolVM')
    const commands = code()
    expect(commands).toContain('lewbox install --public-host 10.0.0.5 --runtime docker --name node-1 -- --lb-health-port=8081')
    expect(commands).toContain('lewbox node add node-1 --new-cluster lab')
    expect(commands).not.toContain('--stop-policy')
  })

  it('links to Settings to add a DNS profile when there is none', async () => {
    stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/dns-profiles': () => json(200, { data: [] }),
      'GET /v0/storage-profiles': () => json(200, { data: [] }),
    })
    renderAt('/', <Route path="/" element={<StartClusterDialog open onClose={() => {}} />} />)
    expect(await screen.findByRole('link', { name: 'add a DNS profile' })).toHaveAttribute('href', '/admin/settings?tab=dns')
  })
})

const domainCluster = {
  id: 'c1', slug: 'eu-west', name: 'EU West', admin_url: '', endpoint_url: '', runtime: 'docker',
  domain: 'eu.api.example.com', dns_profile: 'cf-prod', dns_records: 'auto', storage_profile: 's3-main',
  stop_policy: 'keep', admin_token_last4: '', status: 'ready', nodes: 1, created_at: '',
} as Cluster

describe('AddNodeDialog', () => {
  it('pre-fills the install mode from a domain cluster', async () => {
    stubAPI({ 'GET /v0/organizations': () => json(200, { data: [] }) })
    renderAt(
      '/',
      <Route path="/" element={<AddNodeDialog open onClose={() => {}} clusters={[domainCluster]} cluster={domainCluster} />} />,
    )
    expect(await screen.findByLabelText('Domain')).toHaveValue('eu.api.example.com')
    expect(screen.getByLabelText('DNS profile')).toHaveValue('cf-prod')
    fireEvent.change(screen.getByLabelText('Node name'), { target: { value: 'node-2' } })
    fireEvent.click(screen.getByRole('button', { name: /Next/ }))
    await screen.findByText('Join the cluster')
    const commands = code()
    expect(commands).toContain('lewbox install --domain eu.api.example.com --dns-profile cf-prod --runtime docker --name node-2')
    expect(commands).toContain('lewbox node add node-2 --cluster eu-west --role mixed')
    expect(screen.getByText(/applies the cluster’s storage settings/)).toBeInTheDocument()
  })

  it('asks for the node’s IP on an IP cluster and warns about the join-only path', async () => {
    const ip = { ...domainCluster, domain: '', dns_profile: null }
    stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'POST /v0/clusters/eu-west/join-tokens': () =>
        json(200, { token: 'tok', command: 'sudo install.sh join tok', expires_at: '2099-01-01T00:00:00Z' }),
    })
    renderAt('/', <Route path="/" element={<AddNodeDialog open onClose={() => {}} clusters={[ip]} cluster={ip} />} />)
    fireEvent.change(await screen.findByLabelText('Node name'), { target: { value: 'node-2' } })
    fireEvent.change(screen.getByLabelText('This node’s public IP'), { target: { value: '10.0.0.6' } })
    fireEvent.click(screen.getByRole('button', { name: /Next/ }))
    await screen.findByText('Join the cluster')
    expect(code()).toContain('lewbox install --public-host 10.0.0.6 --runtime docker --name node-2')

    fireEvent.click(screen.getByRole('button', { name: 'Back' }))
    fireEvent.click(await screen.findByRole('radio', { name: /AerolVM’s installer only/ }))
    fireEvent.click(screen.getByRole('button', { name: /Next/ }))
    expect(await screen.findByText(/shows as outdated until/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Create join command' }))
    await waitFor(() => expect(screen.getByText(/Run this on the machine/)).toBeInTheDocument())
  })
})
