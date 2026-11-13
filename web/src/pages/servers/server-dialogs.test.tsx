import { fireEvent, screen, waitFor } from '@testing-library/react'
import { Route } from 'react-router'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { choose, json, renderAt, stubAPI } from '../../test/render'
import { AddNodeDialog } from './add-node-dialog'
import { StartClusterDialog } from './start-cluster-dialog'
import type { Cluster } from '../../lib/api'
import { waitTiming } from './wait-step'

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

// Poll fast, so a test sees the second answer quickly.
beforeEach(() => {
  waitTiming.every = 20
})
afterEach(() => {
  waitTiming.every = 3000
})

const node = (id: string, name: string) => ({ id, name, url: '', created_at: '', cluster: 'eu-west', alive: true })

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
    await choose('Local copies on servers', 'Keep for 7 days')
    fireEvent.click(screen.getByRole('radio', { name: /Sandbox name and ID/ }))
    expect(screen.getByRole('button', { name: /Next/ })).toBeEnabled()
    fireEvent.click(screen.getByRole('button', { name: /Next/ }))

    expect(await screen.findByText('Install AerolVM')).toBeInTheDocument()
    const commands = code()
    expect(commands).toContain(
      'lewbox install --domain eu.api.example.com --dns-profile cf-prod --runtime docker --name node-1',
    )
    expect(commands).toContain(
      'lewbox node add node-1 --new-cluster eu-west --storage-profile s3-main --stop-policy archive --archive-local-retention 7d --archive-snapshot-name name',
    )
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
    expect(commands).not.toContain('--archive-')
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
  stop_policy: 'keep', archive_local_retention: 'instant', archive_snapshot_name: 'id', admin_token_last4: '', status: 'ready', nodes: 1, created_at: '',
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

  it('waits for the named node to join, then says so', async () => {
    let calls = 0
    stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/nodes': () => {
        calls += 1
        const nodes = [node('n1', 'node-1')]
        if (calls > 2) nodes.push(node('n9', 'other'), node('n2', 'node-2'))
        return json(200, { data: nodes })
      },
    })
    renderAt(
      '/',
      <Route path="/" element={<AddNodeDialog open onClose={() => {}} clusters={[domainCluster]} cluster={domainCluster} />} />,
    )
    fireEvent.change(await screen.findByLabelText('Node name'), { target: { value: 'node-2' } })
    fireEvent.click(screen.getByRole('button', { name: /Next/ }))
    expect(await screen.findByText('Waiting for node “node-2” to join “eu-west”…')).toBeInTheDocument()
    expect(await screen.findByText('Node “node-2” joined “eu-west”.')).toBeInTheDocument()
  })

  it('signs in with a token made in a dialog over this one', async () => {
    const calls = stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/nodes': () => json(200, { data: [] }),
      'POST /v0/access-tokens': () =>
        json(201, {
          id: 't1', name: 'Node node-2', prefix: 'lbp_ab', last4: 'wxyz', status: 'active', created_at: '',
          last_used_at: null, expires_at: null, revoked_at: null, token: 'lbp_secret',
        }),
    })
    renderAt(
      '/',
      <Route path="/" element={<AddNodeDialog open onClose={() => {}} clusters={[domainCluster]} cluster={domainCluster} />} />,
    )
    fireEvent.change(await screen.findByLabelText('Node name'), { target: { value: 'node-2' } })
    fireEvent.click(screen.getByRole('button', { name: /Next/ }))
    await screen.findByText('Join the cluster')
    expect(code()).toContain('lewbox login http://localhost:3000 --token')

    fireEvent.click(screen.getByRole('button', { name: 'Create an access token' }))
    expect(await screen.findByLabelText('Name')).toHaveValue('Node node-2')
    fireEvent.click(screen.getByRole('button', { name: 'Create token' }))
    expect(await screen.findByText('Copy your access token')).toBeInTheDocument()
    expect(calls.find((call) => call.method === 'POST')?.body).toMatchObject({ name: 'Node node-2' })

    fireEvent.click(screen.getByRole('button', { name: 'Done' }))
    await waitFor(() => expect(screen.queryByText('Copy your access token')).not.toBeInTheDocument())
    expect(screen.getByText('Join the cluster')).toBeInTheDocument()
  })

  it('says when the node name is already in the cluster', async () => {
    stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/nodes': () => json(200, { data: [node('n1', 'node-1')] }),
    })
    renderAt(
      '/',
      <Route path="/" element={<AddNodeDialog open onClose={() => {}} clusters={[domainCluster]} cluster={domainCluster} />} />,
    )
    fireEvent.change(await screen.findByLabelText('Node name'), { target: { value: 'node-1' } })
    fireEvent.click(screen.getByRole('button', { name: /Next/ }))
    expect(await screen.findByText(/“node-1” is already a node of “eu-west”/)).toBeInTheDocument()
  })
})

describe('StartClusterDialog waiting step', () => {
  async function toCommands() {
    renderAt('/', <Route path="/" element={<StartClusterDialog open onClose={() => {}} />} />)
    fireEvent.change(await screen.findByLabelText('Cluster slug'), { target: { value: 'eu-west' } })
    fireEvent.click(screen.getByRole('radio', { name: /Local/ }))
    fireEvent.click(screen.getByRole('button', { name: /Next/ }))
  }

  it('waits for the cluster to be registered, then links to it', async () => {
    let calls = 0
    stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/dns-profiles': () => json(200, { data: [dnsProfile] }),
      'GET /v0/storage-profiles': () => json(200, { data: [storageProfile] }),
      'GET /v0/clusters/eu-west': () => {
        calls += 1
        return calls > 2 ? json(200, domainCluster) : json(404, { code: 'not_found', message: 'no such cluster' })
      },
    })
    await toCommands()
    expect(await screen.findByText('Waiting for cluster “eu-west” to connect…')).toBeInTheDocument()
    expect(await screen.findByText(/Cluster “eu-west” is connected/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Open it' })).toHaveAttribute('href', '/admin/servers/clusters/eu-west')
  })

  it('says when the slug is already taken', async () => {
    stubAPI({
      'GET /v0/organizations': () => json(200, { data: [] }),
      'GET /v0/dns-profiles': () => json(200, { data: [dnsProfile] }),
      'GET /v0/storage-profiles': () => json(200, { data: [storageProfile] }),
      'GET /v0/clusters/eu-west': () => json(200, domainCluster),
    })
    await toCommands()
    expect(await screen.findByText(/A cluster named “eu-west” already exists here/)).toBeInTheDocument()
  })
})
