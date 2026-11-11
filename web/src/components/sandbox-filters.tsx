import { Listbox, ListboxOption } from '@harlew-dev/ui'
import * as api from '../lib/api'
import { useLoad } from '../lib/use-load'
import { sandboxStatusOptions } from './sandbox-table'

// Region and status pickers for a sandbox list; '' is "all".
export function SandboxFilters({
  region,
  status,
  onRegion,
  onStatus,
}: {
  region: string
  status: string
  onRegion: (region: string) => void
  onStatus: (status: string) => void
}) {
  const regions = useLoad(() => api.listRegions(), [])
  return (
    <div className="mb-6 flex flex-wrap gap-3">
      <Listbox aria-label="Region" value={region} onChange={onRegion} className="sm:w-48">
        <ListboxOption value="">All regions</ListboxOption>
        {(regions.data?.data ?? []).map((option) => (
          <ListboxOption key={option.slug} value={option.slug}>
            {option.name}
          </ListboxOption>
        ))}
      </Listbox>
      <Listbox aria-label="Status" value={status} onChange={onStatus} className="sm:w-48">
        <ListboxOption value="">All statuses</ListboxOption>
        {sandboxStatusOptions.map((option) => (
          <ListboxOption key={option.value} value={option.value}>
            {option.label}
          </ListboxOption>
        ))}
      </Listbox>
    </div>
  )
}
