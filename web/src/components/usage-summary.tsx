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
  if (usage.error.message || (!usage.loading && !usage.data)) {
    return <ErrorNotice error={usage.error} onRetry={onRetry} />
  }
  const totals = usage.data?.totals
  return (
    <>
      <div className="grid gap-8 sm:grid-cols-3">
        <Stat title="vCPU-hours" value={totals ? formatNumber(totals.vcpu_hours, 1) : undefined} />
        <Stat title="Memory GiB-hours" value={totals ? formatNumber(totals.memory_gib_hours, 1) : undefined} />
        <Stat title="Sandbox-hours" value={totals ? formatNumber(totals.sandbox_hours, 1) : undefined} />
      </div>
      {usage.data ? (
        <UsageChart usage={usage.data} regionName={regionName} byRegion={byRegion} />
      ) : (
        <div className="mt-12">
          <ListSkeleton label="Loading usage" rows={3} />
        </div>
      )}
    </>
  )
}
