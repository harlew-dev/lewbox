import { Button, EmptyState, ErrorNotice, ListSkeleton, PageHeader, PageTabs, tabFrom } from '@harlew-dev/ui'
import { useState } from 'react'
import { useSearchParams } from 'react-router'
import * as api from '../../lib/api'
import { SandboxFilters } from '../../components/sandbox-filters'
import { SandboxTable } from '../../components/sandbox-table'
import { Snapshots } from '../../components/snapshot-table'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'
import { usePortalOrg } from './portal-route'

const tabs = [
  { id: 'sandboxes', label: 'Sandboxes' },
  { id: 'snapshots', label: 'Snapshots' },
] as const

// The workspace's sandboxes and, on a second tab, its snapshots.
export function SandboxesPage() {
  const [searchParams] = useSearchParams()
  const tab = tabFrom(searchParams, tabs)
  useDocumentTitle(tab === 'snapshots' ? 'Snapshots' : 'Sandboxes')
  const { org, can } = usePortalOrg()
  const regionName = (slug: string) => org.region?.name ?? slug

  return (
    <>
      <PageHeader title="Sandboxes" />
      <PageTabs label="Sandbox views" tabs={tabs} current={tab} className="mb-8" />
      {tab === 'sandboxes' ? (
        <SandboxList />
      ) : (
        <Snapshots
          org={org.slug}
          canDelete={can.sandboxes}
          regionName={regionName}
          empty={<EmptyState title="No snapshots" description="Snapshots you take show up here." />}
          showRegion={false}
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
