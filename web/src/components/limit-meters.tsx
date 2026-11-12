import { Subheading } from '@harlew-dev/ui'
import { Meter } from '@harlew-dev/ui/chart'
import type { Level } from '@harlew-dev/ui'
import type * as api from '../lib/api'
import { formatMemory, formatNumber } from '../lib/format'
import type { LoadState } from '../lib/use-load'
import { PlanLimits } from './plan-limits'

function level(used: number, limit: number | null): Level {
  if (limit === null) return 'ok'
  if (used >= limit) return 'critical'
  if (used >= limit * 0.8) return 'warn'
  return 'ok'
}

// Live use against each of one region's limits.
export function LimitMeters({ limits, used }: { limits: api.Limits; used: api.LimitsUsed }) {
  const rows = [
    { label: 'Sandboxes', used: used.sandboxes, limit: limits.sandboxes, format: (v: number) => formatNumber(v, 0) },
    { label: 'vCPUs', used: used.vcpus, limit: limits.vcpus, format: (v: number) => formatNumber(v) },
    { label: 'Memory', used: used.memory_mb, limit: limits.memory_mb, format: formatMemory },
  ]
  return (
    <dl className="grid gap-6 sm:grid-cols-3">
      {rows.map(({ label, used, limit, format }) => {
        const text = limit === null ? `${format(used)} · Unlimited` : `${format(used)} of ${format(limit)}`
        return (
          <div key={label}>
            <dt className="flex justify-between gap-2 text-sm/6">
              <span className="font-medium text-zinc-950 dark:text-white">{label}</span>
              <span className="text-zinc-500 tabular-nums dark:text-zinc-400">{text}</span>
            </dt>
            <dd className="mt-2">
              <Meter label={label} value={used} max={limit} level={level(used, limit)} valueText={text} />
            </dd>
          </div>
        )
      })}
    </dl>
  )
}

// The workspace's limits; a workspace has exactly one region.
export function RegionLimits({ limits }: { limits: LoadState<api.LimitsReport> }) {
  return (
    <>
      <Subheading className="mt-14 mb-6">Limits</Subheading>
      <PlanLimits limits={limits} />
    </>
  )
}
