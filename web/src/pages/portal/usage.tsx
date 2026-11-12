import { Listbox, ListboxOption, PageHeader, Subheading } from '@harlew-dev/ui'
import { useState } from 'react'
import * as api from '../../lib/api'
import { PlanLimits } from '../../components/plan-limits'
import { UsageSummary } from '../../components/usage-summary'
import { daysAgo, defaultUsageDays } from '../../lib/format'
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
  const [days, setDays] = useState(String(defaultUsageDays))
  const usage = useLoad(
    () => api.getUsage({ org: org.slug, from: daysAgo(Number(days)), granularity: 'day' }),
    [org.slug, days],
  )
  const limits = useLoad(() => api.getLimits(org.slug), [org.slug])

  return (
    <>
      <PageHeader title="Usage" actions={<UsageRange value={days} onChange={setDays} />} />
      <UsageSummary usage={usage} regionName={(slug) => org.region?.name ?? slug} onRetry={usage.reload} byRegion={false} />
      <Subheading className="mt-14 mb-6">Plan</Subheading>
      <PlanLimits limits={limits} />
    </>
  )
}
