import { Listbox, ListboxOption, PageHeader, Subheading } from '@harlew-dev/ui'
import { useSearchParams } from 'react-router'
import * as api from '../../lib/api'
import { PlanLimits } from '../../components/plan-limits'
import { UsageSummary } from '../../components/usage-summary'
import { daysAgo, defaultUsageDays } from '../../lib/format'
import { oneOf, withParam } from '../../lib/query-params'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'
import { usePortalOrg } from './portal-route'

// Not the library's TimeRange: that always offers a custom range, which
// usage has no use for.
const usageRanges = [
  { days: '7', label: 'Last 7 days' },
  { days: '30', label: 'Last 30 days' },
  { days: '90', label: 'Last 90 days' },
]

function UsageRange({ value, onChange }: { value: string; onChange: (days: string) => void }) {
  return (
    <Listbox aria-label="Range" value={value} onChange={onChange} className="sm:w-44">
      {usageRanges.map((range) => (
        <ListboxOption key={range.days} value={range.days}>
          {range.label}
        </ListboxOption>
      ))}
    </Listbox>
  )
}

export function UsagePage() {
  useDocumentTitle('Usage')
  const { org } = usePortalOrg()
  const [searchParams, setSearchParams] = useSearchParams()
  const days = oneOf(
    searchParams,
    'days',
    usageRanges.map((range) => range.days),
    String(defaultUsageDays),
  )
  const usage = useLoad(`usage:${org.slug}:${days}`, () =>
    api.getUsage({ org: org.slug, from: daysAgo(Number(days)), granularity: 'day' }),
  )
  const limits = useLoad(`limits:${org.slug}`, () => api.getLimits(org.slug))

  function chooseDays(value: string) {
    setSearchParams(
      (current) => withParam(current, 'days', value === String(defaultUsageDays) ? '' : value),
      { preventScrollReset: true },
    )
  }

  return (
    <>
      <PageHeader title="Usage" actions={<UsageRange value={days} onChange={chooseDays} />} />
      <div
        aria-busy={usage.stale || undefined}
        className={usage.stale ? 'opacity-60 transition-opacity' : undefined}
      >
        <UsageSummary
          usage={usage}
          regionName={(slug) => org.region?.name ?? slug}
          onRetry={usage.reload}
          byRegion={false}
        />
      </div>
      <Subheading className="mt-14 mb-6">Plan</Subheading>
      <PlanLimits limits={limits} />
    </>
  )
}
