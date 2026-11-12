import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@harlew-dev/ui'
import type { ReactNode } from 'react'
import type * as api from '../lib/api'
import { formatLimit, formatMemory, formatNumber } from '../lib/format'

// Cluster, plan and the limits there. `reports` (the limits report by region
// slug) puts what is in use now beside each limit, and says when the limits
// couldn't be pushed to the cluster; `actions` adds a last cell per row.
export function SubscriptionTable({
  subscriptions,
  reports,
  actions,
}: {
  subscriptions: api.Subscription[]
  reports?: Map<string, api.RegionLimits>
  actions?: (subscription: api.Subscription) => ReactNode
}) {
  const overridden = (subscription: api.Subscription, key: keyof api.Limits) =>
    subscription.overrides?.[key] != null ? ' (override)' : ''
  const cell = (subscription: api.Subscription, key: keyof api.Limits, format: (value: number) => string) => {
    const now = reports?.get(subscription.region.slug)?.used
    const limit = formatLimit(subscription.limits[key], format)
    return now ? `${format(now[key])} of ${limit}` : limit
  }
  return (
    <Table>
      <TableHead>
        <TableRow>
          <TableHeader>Cluster</TableHeader>
          <TableHeader>Plan</TableHeader>
          <TableHeader className="text-right">Sandboxes</TableHeader>
          <TableHeader className="text-right">vCPUs</TableHeader>
          <TableHeader className="text-right">Memory</TableHeader>
          {actions && (
            <TableHeader className="w-0">
              <span className="sr-only">Actions</span>
            </TableHeader>
          )}
        </TableRow>
      </TableHead>
      <TableBody>
        {subscriptions.map((subscription) => (
          <TableRow key={subscription.region.slug}>
            <TableCell>
              <div className="font-medium">{subscription.region.name}</div>
              {reports?.get(subscription.region.slug)?.error && (
                <div className="text-xs/5 text-red-600 dark:text-red-500">
                  Quota push failed: {reports.get(subscription.region.slug)!.error}
                </div>
              )}
            </TableCell>
            <TableCell>{subscription.plan.name}</TableCell>
            <TableCell className="text-right tabular-nums">
              {cell(subscription, 'sandboxes', (v) => formatNumber(v, 0))}
              {overridden(subscription, 'sandboxes')}
            </TableCell>
            <TableCell className="text-right tabular-nums">
              {cell(subscription, 'vcpus', (v) => formatNumber(v))}
              {overridden(subscription, 'vcpus')}
            </TableCell>
            <TableCell className="text-right tabular-nums">
              {cell(subscription, 'memory_mb', formatMemory)}
              {overridden(subscription, 'memory_mb')}
            </TableCell>
            {actions && <TableCell className="w-0 text-right">{actions(subscription)}</TableCell>}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
