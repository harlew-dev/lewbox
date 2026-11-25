import { Button, EmptyState, ErrorNotice, ListSkeleton, Notice, PageHeader, PageTabs, TextLink, tabFrom } from '@harlew-dev/ui'
import { useSearchParams } from 'react-router'
import * as api from '../../lib/api'
import { SandboxFilters } from '../../components/sandbox-filters'
import { SandboxTable, sandboxStatusOptions } from '../../components/sandbox-table'
import { Snapshots } from '../../components/snapshot-table'
import { oneOf, withParam } from '../../lib/query-params'
import { useRegions } from '../../lib/regions'
import { routes } from '../../lib/routes'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'
import { usePortalOrg } from './portal-route'

const tabs = [
  { id: 'sandboxes', label: 'Sandboxes' },
  { id: 'snapshots', label: 'Snapshots' },
] as const

// The workspace's sandboxes and, on a further tab, its snapshots and archived
// sandboxes.
export function SandboxesPage() {
  const [searchParams] = useSearchParams()
  const { org, can } = usePortalOrg()
  const regions = useRegions(org.slug)
  const regionName = (slug: string) => org.region?.name ?? slug
  const region = regions.list.find((item) => item.slug === org.region?.slug)
  const archiving = region?.stop_policy === 'archive'
  const tab = tabFrom(searchParams, tabs)
  useDocumentTitle(tab === 'snapshots' ? 'Snapshots' : 'Sandboxes')

  return (
    <>
      <PageHeader title="Sandboxes" />
      <PageTabs label="Sandbox views" tabs={tabs} current={tab} className="mb-8" />
      {tab === 'sandboxes' ? (
        <>
          {archiving && (
            <Notice variant="info" className="mb-6">
              Stopped sandboxes in {regionName(region?.slug ?? '')} are archived to storage and come back as new
              sandboxes. <TextLink href={routes.docs('sandboxes/archived-sandboxes')}>How archiving works</TextLink>
            </Notice>
          )}
          <SandboxList />
        </>
      ) : (
        <Snapshots
          org={org.slug}
          canChange={can.sandboxes}
          regionName={regionName}
          empty={
            <EmptyState
              title="No snapshots"
              description="Snapshots you take and sandboxes archived on stop show up here."
            />
          }
          showRegion={false}
        />
      )}
    </>
  )
}

function SandboxList() {
  const { org, can } = usePortalOrg()
  const [searchParams, setSearchParams] = useSearchParams()
  const status = oneOf(
    searchParams,
    'status',
    sandboxStatusOptions.map((option) => option.value),
  )
  const sandboxes = useLoad(
    `sandboxes:${org.slug}:${status || 'all'}`,
    () => api.listSandboxes({ org: org.slug, status }),
    { every: (data) => (data?.data.some((sandbox) => sandbox.status === 'creating') ? 10_000 : 0) },
  )
  const list = sandboxes.data?.data ?? []
  const filtered = status !== ''

  function chooseStatus(value: string) {
    setSearchParams((current) => withParam(current, 'status', value), { preventScrollReset: true })
  }

  return (
    <>
      <SandboxFilters status={status} onStatus={chooseStatus} />
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
              filtered ? (
                <EmptyState title="No sandboxes match" description="Try another status." />
              ) : (
                <EmptyState
                  title="No sandboxes"
                  description="Sandboxes you start with the SDK show up here."
                  action={<Button href={routes.keys()}>Get an API key</Button>}
                />
              )
            ) : (
              <SandboxTable
                sandboxes={list}
                regionName={(slug) => org.region?.name ?? slug}
                canChange={can.sandboxes}
                org={org.slug}
                onChanged={sandboxes.refresh}
                showRegion={false}
              />
            )}
          </div>
        </>
      )}
    </>
  )
}
