import { ErrorNotice, ListSkeleton, Stat } from '@harlew-dev/ui'
import type * as api from '../lib/api'
import { formatNumber } from '../lib/format'
import { UsageChart } from './usage-chart'

// A period's totals as tiles, with the daily chart under them.
export function UsageSummary({
  usage,
  regionName,
  onRetry,
  byRegion = true,
}: {
  usage: { data: api.Usage | null; loading: boolean; error: { message: string; key: number } }
  regionName: (slug: string) => string
  onRetry: () => void
  byRegion?: boolean
}) {
  if (!usage.data) {
    if (usage.error.message) return <ErrorNotice error={usage.error} onRetry={onRetry} />
    return (
      <div className="mt-12">
        <ListSkeleton label="Loading usage" rows={3} />
      </div>
    )
  }
  const totals = usage.data.totals
  return (
    <>
      {usage.error.message && <ErrorNotice error={usage.error} onRetry={onRetry} className="mb-6" />}
      <div className="grid gap-8 sm:grid-cols-3">
        <Stat title="vCPU-hours" value={formatNumber(totals.vcpu_hours, 1)} />
        <Stat title="Memory GiB-hours" value={formatNumber(totals.memory_gib_hours, 1)} />
        <Stat title="Sandbox-hours" value={formatNumber(totals.sandbox_hours, 1)} />
      </div>
      <UsageChart usage={usage.data} regionName={regionName} byRegion={byRegion} />
    </>
  )
}
