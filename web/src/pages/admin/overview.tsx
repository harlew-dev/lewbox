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
  const { data, error, loading, reload } = useLoad(() => api.getOverview(), [], { every: 60_000 })

  if (loading && !data) return <ListSkeleton label="Loading the overview" rows={4} />
  if (error.message || !data) return <ErrorNotice error={error} onRetry={reload} />

  const { customers, sandboxes, capacity, clusters } = data
  const healthy = clusters.filter((cluster) => cluster.status === 'ready').length
  const top = data.top_customers.slice(0, 5)

  return (
    <>
      <PageHeader title="Overview" />
      <div className="grid gap-8 sm:grid-cols-2 xl:grid-cols-5">
        <Stat title="Workspaces" value={formatNumber(customers.total, 0)} href="/admin/workspaces">
          {customers.suspended > 0 && <span>{customers.suspended} suspended</span>}
        </Stat>
        <Stat
          title="Pending approval"
          value={formatNumber(customers.pending, 0)}
          href="/admin/workspaces?tab=pending"
          level={customers.pending > 0 ? 'warn' : undefined}
        />
        <Stat
          title="Sandboxes running"
          value={formatNumber(sandboxes.running, 0)}
          href="/admin/sandboxes"
          chart={
            data.running.length > 1 ? (
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
          <span>{formatNumber(sandboxes.stopped, 0)} stopped</span>
        </Stat>
        <Stat
          title="vCPUs in use"
          value={`${formatNumber(capacity.cpu_reserved, 1)} / ${formatNumber(capacity.cpu_cores, 0)}`}
        >
          <span>
            {formatMemory(capacity.memory_used_mb)} of {formatMemory(capacity.memory_total_mb)} memory
          </span>
        </Stat>
        <Stat
          title="Clusters healthy"
          value={`${healthy} / ${clusters.length}`}
          href="/admin/servers"
          level={clusters.length > 0 && healthy < clusters.length ? 'warn' : undefined}
        >
          <span>
            {capacity.nodes_alive} of {capacity.nodes} nodes alive
          </span>
        </Stat>
      </div>

      <Subheading className="mt-14 mb-6">Uptime, last 30 days</Subheading>
      {clusters.length === 0 ? (
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
      {top.length === 0 ? (
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
  )
}
