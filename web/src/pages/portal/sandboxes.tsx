import { Button, EmptyState, ErrorNotice, ListSkeleton, Notice, PageHeader, PageTabs, TextLink, tabFrom } from '@harlew-dev/ui'
import { useState } from 'react'
import { useSearchParams } from 'react-router'
import * as api from '../../lib/api'
import { ArchiveTable } from '../../components/archive-table'
import { SandboxFilters } from '../../components/sandbox-filters'
import { SandboxTable } from '../../components/sandbox-table'
import { Snapshots } from '../../components/snapshot-table'
import { useRegions } from '../../lib/regions'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'
import { usePortalOrg } from './portal-route'

const baseTabs = [
  { id: 'sandboxes', label: 'Sandboxes' },
  { id: 'snapshots', label: 'Snapshots' },
] as const
const archivedTab = { id: 'archived', label: 'Archived' } as const

// The workspace's sandboxes and, on further tabs, its snapshots and, where
// stopped sandboxes are archived, its archives.
export function SandboxesPage() {
  const [searchParams] = useSearchParams()
  const { org, can } = usePortalOrg()
  const regions = useRegions(org.slug)
  const archives = useLoad(() => api.listArchives(org.slug), [org.slug])
  const regionName = (slug: string) => org.region?.name ?? slug
  const region = regions.list.find((item) => item.slug === org.region?.slug)
  const archiving = region?.stop_policy === 'archive'
  const list = archives.data?.data ?? []
  const tabs = archiving || list.length > 0 ? ([...baseTabs, archivedTab] as const) : baseTabs
  const tab = tabFrom(searchParams, tabs)
  useDocumentTitle(tab === 'snapshots' ? 'Snapshots' : tab === 'archived' ? 'Archived sandboxes' : 'Sandboxes')

  return (
    <>
      <PageHeader title="Sandboxes" />
      <PageTabs label="Sandbox views" tabs={tabs} current={tab} className="mb-8" />
      {tab === 'sandboxes' ? (
        <>
          {archiving && (
            <Notice variant="info" className="mb-6">
              Stopped sandboxes in {regionName(region?.slug ?? '')} are archived to storage and come back as new
              sandboxes. <TextLink href="/docs/sandboxes/archived-sandboxes">How archiving works</TextLink>
            </Notice>
          )}
          <SandboxList />
        </>
      ) : tab === 'snapshots' ? (
        <Snapshots
          org={org.slug}
          canDelete={can.sandboxes}
          regionName={regionName}
          empty={<EmptyState title="No snapshots" description="Snapshots you take show up here." />}
          showRegion={false}
        />
      ) : archives.loading && !archives.data ? (
        <ListSkeleton label="Loading archived sandboxes" rows={3} />
      ) : archives.error.message ? (
        <ErrorNotice error={archives.error} onRetry={archives.reload} />
      ) : list.length === 0 ? (
        <EmptyState
          title="No archived sandboxes"
          description="When a sandbox stops, it is archived to storage and listed here. Restore it to start a new sandbox from it."
        />
      ) : (
        <ArchiveTable
          archives={list}
          canChange={can.sandboxes}
          org={org.slug}
          regionName={regionName}
          onChanged={archives.refresh}
        />
      )}
    </>
  )
}

function SandboxList() {
  const { org, can } = usePortalOrg()
  const [status, setStatus] = useState('')
  const sandboxes = useLoad(() => api.listSandboxes({ org: org.slug, status }), [org.slug, status], {
    every: 10_000,
  })
  const list = sandboxes.data?.data ?? []
  const filtered = status !== ''

  return (
    <>
      <SandboxFilters status={status} onStatus={setStatus} />
      {sandboxes.loading && !sandboxes.data ? (
        <ListSkeleton label="Loading sandboxes" rows={4} />
      ) : sandboxes.error.message ? (
        <ErrorNotice error={sandboxes.error} onRetry={sandboxes.reload} />
      ) : list.length === 0 ? (
        filtered ? (
          <EmptyState title="No sandboxes match" description="Try another status." />
        ) : (
          <EmptyState
            title="No sandboxes"
            description="Sandboxes you start with the SDK show up here."
            action={<Button href="/keys">Get an API key</Button>}
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
    </>
  )
}
