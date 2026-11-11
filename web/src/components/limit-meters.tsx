import { Meter } from '@harlew-dev/ui/chart'
import type { Level } from '@harlew-dev/ui'
import type * as api from '../lib/api'
import { formatMemory, formatNumber } from '../lib/format'

function level(used: number, limit: number): Level {
  if (used >= limit) return 'critical'
  if (used >= limit * 0.8) return 'warn'
  return 'ok'
}

// Live use against each limit, across every region together.
export function LimitMeters({ report }: { report: api.LimitsReport }) {
  const rows = [
    { label: 'Sandboxes', used: report.used.sandboxes, limit: report.limits.sandboxes, format: (v: number) => formatNumber(v, 0) },
    { label: 'vCPUs', used: report.used.vcpus, limit: report.limits.vcpus, format: (v: number) => formatNumber(v) },
    { label: 'Memory', used: report.used.memory_mb, limit: report.limits.memory_mb, format: formatMemory },
  ]
  return (
    <dl className="grid gap-6 sm:grid-cols-3">
      {rows.map(({ label, used, limit, format }) => {
        const text = limit === null ? `${format(used)} · no limit` : `${format(used)} of ${format(limit)}`
        return (
          <div key={label}>
            <dt className="flex justify-between gap-2 text-sm/6">
              <span className="font-medium text-zinc-950 dark:text-white">{label}</span>
              <span className="text-zinc-500 tabular-nums dark:text-zinc-400">{text}</span>
            </dt>
            <dd className="mt-2">
              {limit === null ? (
                <div className="h-2 rounded-full bg-zinc-950/5 dark:bg-white/10" />
              ) : (
                <Meter label={label} value={used} max={limit} level={level(used, limit)} valueText={text} />
              )}
            </dd>
          </div>
        )
      })}
    </dl>
  )
}
