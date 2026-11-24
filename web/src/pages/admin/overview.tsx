import { ErrorNotice, ListSkeleton, PageHeader, Stat, Subheading, Text, TextLink } from '@harlew-dev/ui'
import { BarList, Sparkline } from '@harlew-dev/ui/chart'
import * as api from '../../lib/api'
import { NoClusters } from '../../components/no-clusters'
import { StatusBadge } from '../../components/status'
import { Uptime } from '../../components/uptime'
import { formatMemory, formatNumber } from '../../lib/format'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'

function hourLabel(value: string): string {
  return new Date(value).toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    timeZone: 'UTC',
  })
}

export function OverviewPage() {
  useDocumentTitle('Overview')
  const { data, error, reload } = useLoad(() => api.getOverview(), [], { every: 60_000 })
  const customers = data?.customers
  const sandboxes = data?.sandboxes
  const capacity = data?.capacity
  const clusters = data?.clusters ?? []
  const healthy = clusters.filter((cluster) => cluster.status === 'ready').length
  const top = data?.top_customers.slice(0, 5) ?? []

  return (
    <>
      <PageHeader title="Overview" />
      {error.message ? (
        <ErrorNotice error={error} onRetry={reload} />
      ) : (
        <>
          <div className="grid gap-8 sm:grid-cols-2 xl:grid-cols-5">
            <Stat
              title="Workspaces"
              value={customers ? formatNumber(customers.total, 0) : undefined}
              href="/admin/workspaces"
            >
              {customers && customers.suspended > 0 && <span>{customers.suspended} suspended</span>}
            </Stat>
            <Stat
              title="Pending approval"
              value={customers ? formatNumber(customers.pending, 0) : undefined}
              href="/admin/workspaces?tab=pending"
              level={customers && customers.pending > 0 ? 'warn' : undefined}
            />
            <Stat
              title="Sandboxes running"
              value={sandboxes ? formatNumber(sandboxes.running, 0) : undefined}
              href="/admin/sandboxes"
              chart={
                data && data.running.length > 1 ? (
                  <Sparkline
                    label="Sandboxes running"
                    summary="Average sandboxes running per hour over the last 7 days."
                    points={data.running.map((point) => ({
                      label: hourLabel(point.start),
                      value: Math.round(point.sandboxes * 10) / 10,
                    }))}
                  />
                ) : undefined
              }
            >
              {sandboxes && <span>{formatNumber(sandboxes.stopped, 0)} stopped</span>}
            </Stat>
            <Stat
              title="vCPUs in use"
              value={
                capacity
                  ? `${formatNumber(capacity.cpu_reserved, 1)} / ${formatNumber(capacity.cpu_cores, 0)}`
                  : undefined
              }
            >
              {capacity && (
                <span>
                  {formatMemory(capacity.memory_used_mb)} of {formatMemory(capacity.memory_total_mb)} memory
                </span>
              )}
            </Stat>
            <Stat
              title="Clusters healthy"
              value={data ? `${healthy} / ${clusters.length}` : undefined}
              href="/admin/servers"
              level={data && clusters.length > 0 && healthy < clusters.length ? 'warn' : undefined}
            >
              {capacity && (
                <span>
                  {capacity.nodes_alive} of {capacity.nodes} nodes alive
                </span>
              )}
            </Stat>
          </div>

          <Subheading className="mt-14 mb-6">Uptime, last 30 days</Subheading>
          {!data ? (
            <ListSkeleton label="Loading uptime" rows={2} />
          ) : clusters.length === 0 ? (
            <NoClusters />
          ) : (
            <div className="space-y-8">
              {clusters.map((cluster) => (
                <div key={cluster.slug}>
                  <div className="mb-3 flex items-center gap-3">
                    <TextLink href={`/admin/servers/clusters/${encodeURIComponent(cluster.slug)}`} className="font-medium">
                      {cluster.name}
                    </TextLink>
                    <StatusBadge status={cluster.status} />
                  </div>
                  <Uptime label={cluster.name} uptime={cluster.uptime} days={cluster.days} />
                </div>
              ))}
            </div>
          )}

          <Subheading className="mt-14 mb-4">Top workspaces this month, in vCPU-hours</Subheading>
          {!data ? (
            <ListSkeleton label="Loading the top workspaces" rows={2} />
          ) : top.length === 0 ? (
            <Text>No usage yet this month.</Text>
          ) : (
            <BarList
              label="Top workspaces by vCPU-hours this month"
              total={top.reduce((sum, customer) => sum + customer.vcpu_hours, 0)}
              items={top.map((customer) => ({
                key: customer.slug,
                label: customer.name,
                value: Math.round(customer.vcpu_hours * 10) / 10,
                href: `/admin/workspaces/${encodeURIComponent(customer.slug)}`,
              }))}
            />
          )}
        </>
      )}
    </>
  )
}
