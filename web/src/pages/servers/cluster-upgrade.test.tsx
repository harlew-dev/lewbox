import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { Route } from 'react-router'
import { afterEach, describe, expect, it } from 'vitest'
import type { ClusterUpgrade, ClusterUpgradePlan } from '../../lib/api'
import { json, renderAt, stubAPI, type Call } from '../../test/render'
import { ClusterDetailPage } from './cluster-detail'
import { upgradeTiming } from './cluster-upgrade'

const cluster = {
  id: 'c1',
  slug: 'eu-west',
  name: 'EU West',
  admin_url: 'http://10.0.0.5:21212',
  endpoint_url: 'http://10.0.0.5:21212',
  runtime: 'docker',
  domain: 'eu.api.example.com',
  dns_profile: 'cf-prod',
  dns_records: 'auto',
  storage_profile: null,
  stop_policy: 'keep',
  archive_local_retention: 'instant',
  archive_snapshot_name: 'id',
  docker_timeout: '15m',
  max_sandbox_cpu: 2,
  max_sandbox_memory_mb: null,
  max_sandbox_disk_gb: 50,
  idle_stop_minutes: 15,
  destroy_if_idle_for: '',
  max_age: '168h',
  firecracker_vm_slots: null,
  admin_token_last4: '9f3a',
  status: 'ready',
  version: 'v0.7.0',
  nodes: 2,
  checked_at: '2026-11-11T15:23:01.2Z',
  created_at: '2026-11-11T15:22:35.8Z',
  checks: [],
  node_list: [],
}

const notFound = () =>
  json(404, { error: { code: 'not_found', message: 'no upgrade has run on cluster "eu-west"' } })

const now = '2026-11-20T10:00:00Z'

function planNode(
  node: string,
  order: number,
  over: Partial<ClusterUpgradePlan['nodes'][number]> = {},
): ClusterUpgradePlan['nodes'][number] {
  return {
    node,
    role: 'worker',
    version: 'v0.7.0',
    target_version: 'v0.7.1',
    alive: true,
    voter: false,
    leader: false,
    order,
    batch: 1,
    action: 'upgrade',
    ...over,
  }
}

function plan(over: Partial<ClusterUpgradePlan> = {}): ClusterUpgradePlan {
  return {
    cluster: 'eu-west',
    cluster_id: 'c1',
    target_version: 'v0.7.1',
    tested: true,
    source: 'https://releases.test/v0.7.1',
    allowed: true,
    refusals: [],
    warnings: [],
    needs_allow_downtime: false,
    skew: { upgrade_from_min: 'v0.6.11', below: [] },
    one_way_migrations: [],
    rollback_after_finalize: true,
    batch: '10%',
    batch_size: 1,
    nodes: [
      planNode('node-2', 1),
      planNode('node-1', 2, { role: 'mixed', voter: true, leader: true }),
    ],
    metadata: {},
    ...over,
  }
}

function node(name: string, state: string, over: Partial<ClusterUpgrade['nodes'][number]> = {}) {
  return {
    node: name,
    role: 'worker',
    from_version: 'v0.7.0',
    to_version: 'v0.7.1',
    state,
    order: 1,
    migrated: false,
    updated_at: now,
    ...over,
  }
}

function rollout(over: Partial<ClusterUpgrade> = {}): ClusterUpgrade {
  return {
    cluster: 'eu-west',
    cluster_id: 'c1',
    id: 'r1',
    phase: 'running',
    direction: 'upgrade',
    target_version: 'v0.7.1',
    previous_version: 'v0.7.0',
    source: 'release',
    hold: false,
    batch: '10%',
    allow_downtime: false,
    created_at: now,
    updated_at: now,
    nodes: [node('node-2', 'done'), node('node-1', 'upgrading')],
    ...over,
  }
}

function routes(extra: Record<string, (call: Call) => Response> = {}) {
  return {
    'GET /v0/organizations': () => json(200, { data: [] }),
    'GET /v0/clusters/eu-west': () => json(200, cluster),
    'GET /v0/clusters/eu-west/upgrade': notFound,
    ...extra,
  }
}

function open() {
  return renderAt(
    '/admin/servers/clusters/eu-west',
    <Route path="/admin/servers/clusters/:ref" element={<ClusterDetailPage />} />,
  )
}

async function openDialog() {
  const button = await screen.findByRole('button', { name: 'Upgrade cluster' })
  // The page header renders its actions before the cluster loads.
  await waitFor(() => expect(button).toBeEnabled())
  fireEvent.click(button)
}

afterEach(() => {
  upgradeTiming.every = 4000
})

describe('UpgradeClusterDialog', () => {
  it('shows the plan in node order and starts the tested release', async () => {
    const calls = stubAPI(
      routes({
        'GET /v0/clusters/eu-west/upgrade/plan': () => json(200, plan()),
        'POST /v0/clusters/eu-west/upgrade': () => json(201, rollout({ phase: 'staging' })),
      }),
    )
    open()
    await openDialog()

    expect(await screen.findByRole('heading', { name: 'Upgrade to v0.7.1' })).toBeInTheDocument()
    const first = screen.getByText('node-2').closest('tr')!
    expect(within(first).getByText('worker')).toBeInTheDocument()
    expect(within(first).getByText('v0.7.0')).toBeInTheDocument()
    expect(within(first).getByText('Upgrade to v0.7.1')).toBeInTheDocument()
    const second = screen.getByText('node-1').closest('tr')!
    expect(within(second).getByText('mixed')).toBeInTheDocument()
    expect(screen.getByText(/The jump is allowed/)).toBeInTheDocument()
    expect(screen.getByText(/Rollback stays possible/)).toBeInTheDocument()

    // The empty field asks for the tested release, with no version query.
    expect(calls.find((call) => call.url.includes('/upgrade/plan'))?.url).toBe(
      '/v0/clusters/eu-west/upgrade/plan',
    )

    fireEvent.click(screen.getByRole('button', { name: 'Start upgrade' }))
    await waitFor(() =>
      expect(calls.find((call) => call.method === 'POST' && call.url === '/v0/clusters/eu-west/upgrade')).toBeDefined(),
    )
    expect(calls.find((call) => call.method === 'POST')?.body).toEqual({
      version: 'v0.7.1',
      batch: '10%',
      allow_downtime: false,
    })
  })

  it('shows why a refused plan cannot start', async () => {
    const refusal =
      'node-1 runs v0.7.1, which is newer than v0.7.0; going back is: lewbox cluster upgrade rollback eu-west'
    stubAPI(
      routes({
        'GET /v0/clusters/eu-west/upgrade/plan': () =>
          json(
            200,
            plan({
              allowed: false,
              refusals: [refusal],
              nodes: [planNode('node-1', 0, { action: 'skip_target' })],
            }),
          ),
      }),
    )
    open()
    await openDialog()

    expect(await screen.findByText(/This upgrade can’t start/)).toBeInTheDocument()
    expect(screen.getByText(refusal)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Start upgrade' })).toBeDisabled()
  })

  it('re-plans a typed version and warns that it is untested', async () => {
    const calls = stubAPI(
      routes({
        'GET /v0/clusters/eu-west/upgrade/plan': (call) =>
          json(
            200,
            call.url.includes('version=v0.7.2')
              ? plan({
                  target_version: 'v0.7.2',
                  tested: false,
                  warnings: [
                    'AerolVM v0.7.2 is not the version this Lewbox release was tested with (v0.7.0); the upgrade is untested',
                  ],
                })
              : plan(),
          ),
      }),
    )
    open()
    await openDialog()
    expect(await screen.findByRole('heading', { name: 'Upgrade to v0.7.1' })).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('AerolVM version'), { target: { value: 'v0.7.2' } })

    expect(await screen.findByRole('heading', { name: 'Upgrade to v0.7.2' })).toBeInTheDocument()
    expect(screen.getByText(/the upgrade is untested/)).toBeInTheDocument()
    expect(screen.getByText('Untested')).toBeInTheDocument()
    expect(calls.some((call) => call.url === '/v0/clusters/eu-west/upgrade/plan?version=v0.7.2')).toBe(true)
  })

  it('asks for allow downtime only when the plan needs it', async () => {
    const calls = stubAPI(
      routes({
        'GET /v0/clusters/eu-west/upgrade/plan': () =>
          json(
            200,
            plan({
              needs_allow_downtime: true,
              warnings: [
                'this cluster has two Raft voters: ... starting it needs downtime allowed (--allow-downtime)',
              ],
            }),
          ),
        'POST /v0/clusters/eu-west/upgrade': () => json(201, rollout({ phase: 'running' })),
      }),
    )
    open()
    await openDialog()

    expect(await screen.findByText('Allow downtime')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Start upgrade' })).toBeDisabled()

    fireEvent.click(screen.getByRole('checkbox', { name: /Allow downtime/ }))
    expect(screen.getByRole('checkbox', { name: /Allow downtime/ })).toBeChecked()
    expect(screen.getByRole('button', { name: 'Start upgrade' })).toBeEnabled()
    fireEvent.click(screen.getByRole('button', { name: 'Start upgrade' }))

    await waitFor(() =>
      expect(calls.find((call) => call.method === 'POST' && call.url === '/v0/clusters/eu-west/upgrade')).toBeDefined(),
    )
    expect(calls.find((call) => call.method === 'POST')?.body).toEqual({
      version: 'v0.7.1',
      batch: '10%',
      allow_downtime: true,
    })
  })
})

describe('ClusterUpgradeSection', () => {
  it('shows each node’s state and the phase', async () => {
    stubAPI(
      routes({
        'GET /v0/clusters/eu-west/upgrade': () =>
          json(
            200,
            rollout({
              nodes: [
                node('node-2', 'done'),
                node('node-1', 'upgrading', { role: 'mixed' }),
                node('node-3', 'failed', { error: 'host setup failed' }),
                node('node-4', 'pending'),
              ],
            }),
          ),
      }),
    )
    open()

    expect(await screen.findByRole('heading', { name: 'Upgrade' })).toBeInTheDocument()
    expect(screen.getByText('Running')).toBeInTheDocument()
    expect(screen.getByText('Target v0.7.1')).toBeInTheDocument()

    expect(within(screen.getByText('node-2').closest('tr')!).getByText('Upgraded')).toBeInTheDocument()
    expect(within(screen.getByText('node-1').closest('tr')!).getByText('Upgrading')).toBeInTheDocument()
    const failed = screen.getByText('node-3').closest('tr')!
    expect(within(failed).getByText('Failed')).toBeInTheDocument()
    expect(within(failed).getByText('host setup failed')).toBeInTheDocument()
    expect(within(screen.getByText('node-4').closest('tr')!).getByText('Waiting')).toBeInTheDocument()

    expect(screen.getByRole('button', { name: 'Stop upgrade' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Finalize upgrade' })).toBeNull()
  })

  it('offers only the commands the phase allows, with confirmations', async () => {
    const calls = stubAPI(
      routes({
        'GET /v0/clusters/eu-west/upgrade': () =>
          json(200, rollout({ phase: 'done', hold: true, nodes: [node('node-1', 'done')] })),
        'POST /v0/clusters/eu-west/upgrade/finalize': () =>
          json(200, rollout({ phase: 'finalized', nodes: [node('node-1', 'done')] })),
      }),
    )
    open()

    expect(await screen.findByRole('heading', { name: 'Upgrade' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Stop upgrade' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Resume upgrade' })).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Roll back' }))
    expect(await screen.findByText('Roll back this upgrade?')).toBeInTheDocument()
    expect(screen.getByText(/Every node goes back to v0.7.0/)).toBeInTheDocument()
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }))

    fireEvent.click(screen.getByRole('button', { name: 'Finalize upgrade' }))
    expect(await screen.findByText('Finalize this upgrade?')).toBeInTheDocument()
    expect(screen.getByText(/Rollback will no longer be possible/)).toBeInTheDocument()
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Finalize upgrade' }))

    await waitFor(() =>
      expect(calls.find((call) => call.url === '/v0/clusters/eu-west/upgrade/finalize')).toBeDefined(),
    )
  })

  it('polls while the rollout runs and stops once it is finalized', async () => {
    upgradeTiming.every = 20
    let reads = 0
    const calls = stubAPI(
      routes({
        'GET /v0/clusters/eu-west/upgrade': () => {
          reads += 1
          return json(
            200,
            reads === 1
              ? rollout()
              : rollout({
                  phase: 'finalized',
                  nodes: [node('node-2', 'done'), node('node-1', 'done')],
                }),
          )
        },
        'POST /v0/clusters/eu-west/check': () => json(200, { ...cluster, version: 'v0.7.1' }),
      }),
    )
    open()

    expect(await screen.findByText(/Upgraded to v0\.7\.1 on/)).toBeInTheDocument()
    // The page checks the cluster as the upgrade settles, so its version is current.
    await waitFor(() => expect(calls.some((call) => call.method === 'POST' && call.url === '/v0/clusters/eu-west/check')).toBe(true))
    const answered = calls.filter((call) => call.url === '/v0/clusters/eu-west/upgrade').length
    expect(answered).toBe(2)

    await new Promise((resolve) => window.setTimeout(resolve, 80))
    expect(calls.filter((call) => call.url === '/v0/clusters/eu-west/upgrade')).toHaveLength(answered)
  })

  it('shows no section when the cluster never had an upgrade', async () => {
    const calls = stubAPI(routes())
    open()

    expect(await screen.findByRole('heading', { name: 'EU West' })).toBeInTheDocument()
    await waitFor(() => expect(calls.some((call) => call.url === '/v0/clusters/eu-west/upgrade')).toBe(true))
    expect(screen.queryByRole('heading', { name: 'Upgrade' })).toBeNull()
  })
})
