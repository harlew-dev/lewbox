import { EmptyState, ErrorNotice, ListSkeleton, PageHeader } from '@harlew-dev/ui'
import { useSearchParams } from 'react-router'
import * as api from '../../lib/api'
import { SandboxFilters } from '../../components/sandbox-filters'
import { SandboxTable, sandboxStatusOptions } from '../../components/sandbox-table'
import { one, oneOf, withParam } from '../../lib/query-params'
import { useRegions } from '../../lib/regions'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'

// Running first, the longest-running at the top: what to look at first
// when something is being abused.
function byLongestRunning(a: api.Sandbox, b: api.Sandbox): number {
  const running = Number(b.status === 'started') - Number(a.status === 'started')
  return running || a.created_at.localeCompare(b.created_at)
}

export function FleetSandboxesPage() {
  useDocumentTitle('Sandboxes')
  const [searchParams, setSearchParams] = useSearchParams()
  const region = one(searchParams, 'region')
  const status = oneOf(
    searchParams,
    'status',
    sandboxStatusOptions.map((option) => option.value),
  )
  const sandboxes = useLoad(
    `sandboxes:${region || 'all'}:${status || 'all'}`,
    () => api.listSandboxes({ region, status }),
    { every: (data) => (data?.data.some((sandbox) => sandbox.status === 'creating') ? 10_000 : 0) },
  )
  const list = [...(sandboxes.data?.data ?? [])].sort(byLongestRunning)
  const regions = useRegions()
  const filtered = region !== '' || status !== ''

  function choose(key: string, value: string) {
    setSearchParams((current) => withParam(current, key, value), { preventScrollReset: true })
  }

  return (
    <>
      <PageHeader title="Sandboxes" description="Every workspace’s sandboxes, longest running first." />
      <SandboxFilters
        regions={regions.list}
        region={region}
        status={status}
        onRegion={(value) => choose('region', value)}
        onStatus={(value) => choose('status', value)}
      />
      {sandboxes.error.message && !sandboxes.data ? (
        <ErrorNotice error={sandboxes.error} onRetry={sandboxes.reload} />
      ) : (
        <>
          {sandboxes.error.message && (
            <ErrorNotice error={sandboxes.error} onRetry={sandboxes.reload} className="mb-6" />
          )}
          <div
            aria-busy={sandboxes.stale || undefined}
            className={sandboxes.stale ? 'opacity-60 transition-opacity' : undefined}
          >
            {sandboxes.loading && !sandboxes.data ? (
              <ListSkeleton label="Loading sandboxes" rows={4} />
            ) : list.length === 0 ? (
              <EmptyState
                title={filtered ? 'No sandboxes match' : 'No sandboxes'}
                description={
                  filtered
                    ? 'Try another region or status.'
                    : 'Sandboxes workspaces start show up here within seconds.'
                }
              />
            ) : (
              <SandboxTable sandboxes={list} regionName={regions.name} fleet canChange onChanged={sandboxes.refresh} />
            )}
          </div>
        </>
      )}
    </>
  )
}
