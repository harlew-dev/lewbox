import { ErrorNotice, ListSkeleton, Text } from '@harlew-dev/ui'
import type * as api from '../lib/api'
import type { LoadState } from '../lib/use-load'
import { LimitMeters } from './limit-meters'
import { noRegionText } from './no-region'

// The workspace's plan in its one region, with a meter per limit. `heading`
// names the plan and region above the meters.
export function PlanLimits({ limits, heading = true }: { limits: LoadState<api.LimitsReport>; heading?: boolean }) {
  if (limits.loading && !limits.data) return <ListSkeleton label="Loading limits" rows={1} />
  if (limits.error.message) return <ErrorNotice error={limits.error} onRetry={limits.reload} />
  const region = limits.data?.regions[0]
  if (!region) return <Text>{noRegionText}</Text>
  return (
    <div>
      {heading && (
        <div className="mb-4 flex items-baseline gap-2 text-sm/6">
          <span className="font-semibold text-zinc-950 dark:text-white">{region.plan.name} plan</span>
          <span className="text-zinc-500 dark:text-zinc-400">in {region.name}</span>
        </div>
      )}
      <LimitMeters limits={region.limits} used={region.used} />
      {region.error && (
        <p className="mt-4 text-sm/6 text-red-600 dark:text-red-500">Limits couldn’t be pushed: {region.error}</p>
      )}
    </div>
  )
}
