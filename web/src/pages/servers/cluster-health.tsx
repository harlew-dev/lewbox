import { ErrorNotice, Listbox, ListboxOption, ListSkeleton, Stat, Subheading, Text } from '@harlew-dev/ui'
import { Sparkline } from '@harlew-dev/ui/chart'
import { useState } from 'react'
import { Uptime } from '../../components/uptime'
import * as api from '../../lib/api'
import { formatMemory, formatNumber } from '../../lib/format'
import { useLoad } from '../../lib/use-load'

// 30-day uptime for the cluster and each of its nodes.
export function ClusterUptime({ cluster }: { cluster: string }) {
  const health = useLoad(() => api.clusterHealth(cluster), [cluster])
  return (
    <>
      <Subheading className="mt-12 mb-6">Uptime, last 30 days</Subheading>
      {health.loading && !health.data ? (
        <ListSkeleton label="Loading uptime" rows={2} />
      ) : health.error.message || !health.data ? (
        <ErrorNotice error={health.error} onRetry={health.reload} />
      ) : (
        <div className="space-y-8">
          <div>
            <div className="mb-3 text-sm/6 font-medium text-zinc-950 dark:text-white">Cluster</div>
            <Uptime label="Cluster" uptime={health.data.cluster.uptime} days={health.data.cluster.days} />
          </div>
          {health.data.nodes.map((node) => (
            <div key={node.name}>
              <div className="mb-3 text-sm/6 font-medium text-zinc-950 dark:text-white">{node.name}</div>
              <Uptime label={node.name} uptime={node.uptime} days={node.days} />
            </div>
          ))}
        </div>
      )}
    </>
  )
}

const ranges: { id: api.MetricsRange; label: string }[] = [
  { id: '24h', label: 'Last 24 hours' },
  { id: '7d', label: 'Last 7 days' },
  { id: '30d', label: 'Last 30 days' },
]

function pointLabel(value: string): string {
  return new Date(value).toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    timeZone: 'UTC',
  })
}

// Each node's sandboxes, reserved vCPUs and memory in use over time.
export function ClusterMetrics({ cluster }: { cluster: string }) {
  const [range, setRange] = useState<api.MetricsRange>('24h')
  const metrics = useLoad(() => api.clusterMetrics(cluster, range), [cluster, range])

  return (
    <>
      <div className="mt-12 mb-6 flex flex-wrap items-end justify-between gap-4">
        <Subheading>Node metrics</Subheading>
        <Listbox aria-label="Range" value={range} onChange={setRange} className="sm:w-44">
          {ranges.map((option) => (
            <ListboxOption key={option.id} value={option.id}>
              {option.label}
            </ListboxOption>
          ))}
        </Listbox>
      </div>
      {metrics.loading && !metrics.data ? (
        <ListSkeleton label="Loading metrics" rows={2} />
      ) : metrics.error.message || !metrics.data ? (
        <ErrorNotice error={metrics.error} onRetry={metrics.reload} />
      ) : metrics.data.nodes.length === 0 ? (
        <Text>No samples yet.</Text>
      ) : (
        <div className="space-y-10">
          {metrics.data.nodes.map((node) => {
            const last = node.data.at(-1)
            const series = (value: (point: api.NodeMetric) => number) =>
              node.data.map((point) => ({ label: pointLabel(point.start), value: Math.round(value(point) * 10) / 10 }))
            return (
              <div key={node.name}>
                <div className="text-sm/6 font-medium text-zinc-950 dark:text-white">{node.name}</div>
                <div className="grid gap-8 sm:grid-cols-3">
                  <Stat
                    title="Sandboxes"
                    value={last ? formatNumber(last.sandboxes, 0) : undefined}
                    chart={<Sparkline label="Sandboxes" summary={`Sandboxes on ${node.name}.`} points={series((p) => p.sandboxes)} />}
                  />
                  <Stat
                    title="vCPUs reserved"
                    value={last ? `${formatNumber(last.cpu_reserved, 1)} / ${last.cpu_cores}` : undefined}
                    chart={<Sparkline label="vCPUs reserved" summary={`vCPUs reserved on ${node.name}.`} points={series((p) => p.cpu_reserved)} />}
                  />
                  <Stat
                    title="Memory in use"
                    value={last ? `${formatMemory(last.memory_used_mb)} / ${formatMemory(last.memory_total_mb)}` : undefined}
                    chart={
                      <Sparkline
                        label="Memory in use"
                        summary={`Memory in use on ${node.name}, in MB.`}
                        points={series((p) => p.memory_used_mb)}
                      />
                    }
                  />
                </div>
              </div>
            )
          })}
        </div>
      )}
    </>
  )
}
