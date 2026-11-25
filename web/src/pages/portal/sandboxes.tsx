import { Button, EmptyState, ErrorNotice, Notice, PageHeader, PageTabs, SearchField, TextLink, tabFrom } from '@harlew-dev/ui'
import { useSearchParams } from 'react-router'
import type { ReactNode } from 'react'
import * as api from '../../lib/api'
import { FilterBar, type FilterDef } from '../../components/filter-bar'
import { NoMatches, NoRecords } from '../../components/list'
import { SandboxTable, sandboxStatusOptions } from '../../components/sandbox-table'
import { PartialNotice, SnapshotTable, snapshotPage, useSnapshots } from '../../components/snapshot-table'
import { isFiltered, listQuery, type ListSpec } from '../../lib/list-query'
import { useRegions } from '../../lib/regions'
import { routes } from '../../lib/routes'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useListState } from '../../lib/use-list'
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
        <SnapshotList
          org={org.slug}
          canChange={can.sandboxes}
          regionName={regionName}
          empty={
            <EmptyState
              title="No snapshots"
              description="Snapshots you take and sandboxes archived on stop show up here."
            />
          }
        />
      )}
    </>
  )
}

const sandboxSpec: ListSpec = {
  sortKeys: ['created', 'status'],
  defaultSort: '-created',
  descendingFirst: ['created'],
  filters: { status: sandboxStatusOptions.map((option) => option.value) },
}

const sandboxFilters: FilterDef[] = [
  { key: 'status', label: 'Status', options: sandboxStatusOptions },
]

function SandboxList() {
  const { org, can } = usePortalOrg()
  const list = useListState(sandboxSpec)
  const { state } = list
  const sandboxes = useLoad(
    `sandboxes:${org.slug}:${list.key()}`,
    () =>
      api.listSandboxes({
        ...listQuery(state, sandboxSpec),
        org: org.slug,
        status: state.filters.status,
      }),
    {
      every: (data) =>
        data?.data.some((sandbox) => sandbox.status === 'creating') ? 10_000 : 0,
    },
  )

  return (
    <div className="space-y-4">
      <FilterBar
        filters={sandboxFilters}
        values={state.filters}
        onApply={list.setFilter}
        onRemove={list.removeFilter}
        onClear={list.clearFilters}
        leading={
          <SearchField
            label="Search sandboxes"
            value={state.q}
            onSearch={list.setSearch}
          />
        }
      />
      {sandboxes.error.message && sandboxes.data && (
        <ErrorNotice error={sandboxes.error} onRetry={sandboxes.reload} />
      )}
      {sandboxes.error.message && !sandboxes.data ? (
        <ErrorNotice error={sandboxes.error} onRetry={sandboxes.reload} />
      ) : (
        <SandboxTable
          page={sandboxes.data ?? undefined}
          loading={sandboxes.loading || sandboxes.stale}
          label="Sandboxes"
          sort={state.sort}
          sortHref={(sort) => list.href({ sort, page: 1 })}
          href={list.href}
          regionName={() => org.region?.name ?? ''}
          canChange={can.sandboxes}
          org={org.slug}
          onChanged={sandboxes.refresh}
          showRegion={false}
          empty={
            isFiltered(state) ? (
              <NoMatches label="sandboxes" onClear={list.clearAll} />
            ) : (
              <NoRecords
                title="No sandboxes"
                description="Sandboxes you start with the SDK show up here."
                action={<Button href={routes.keys()}>Get an API key</Button>}
              />
            )
          }
        />
      )}
    </div>
  )
}

const snapshotSpec: ListSpec = {
  sortKeys: ['created', 'name', 'size'],
  defaultSort: '-created',
  descendingFirst: ['created', 'size'],
  filters: {
    kind: ['snapshot', 'archive'],
    status: ['pending', 'pushing', 'active', 'error'],
  },
}

const snapshotFilters: FilterDef[] = [
  {
    key: 'kind',
    label: 'Kind',
    options: [
      { value: 'snapshot', label: 'Snapshots' },
      { value: 'archive', label: 'Archived sandboxes' },
    ],
  },
  {
    key: 'status',
    label: 'Status',
    options: [
      { value: 'pending', label: 'Pending' },
      { value: 'pushing', label: 'Pushing' },
      { value: 'active', label: 'Active' },
      { value: 'error', label: 'Error' },
    ],
  },
]

// The workspace's snapshots and archived sandboxes.
function SnapshotList({
  org,
  canChange,
  regionName,
  empty,
}: {
  org: string
  canChange: boolean
  regionName: (slug: string) => string
  empty: ReactNode
}) {
  const list = useListState(snapshotSpec)
  const { state } = list
  const snapshots = useSnapshots(
    org,
    {
      ...listQuery(state, snapshotSpec),
      kind: state.filters.kind as api.SnapshotKind[] | undefined,
      status: state.filters.status as api.SnapshotStatus[] | undefined,
    },
    list.key(),
  )
  const { load } = snapshots

  return (
    <div className="space-y-4">
      <FilterBar
        filters={snapshotFilters}
        values={state.filters}
        onApply={list.setFilter}
        onRemove={list.removeFilter}
        onClear={list.clearFilters}
        leading={
          <SearchField
            label="Search snapshots"
            value={state.q}
            onSearch={list.setSearch}
          />
        }
      />
      <PartialNotice partial={snapshots.partial} />
      {load.error.message && load.data && (
        <ErrorNotice error={load.error} onRetry={load.reload} />
      )}
      {load.error.message && !load.data ? (
        <ErrorNotice error={load.error} onRetry={load.reload} />
      ) : (
        <SnapshotTable
          page={snapshotPage(snapshots)}
          loading={load.loading || load.stale}
          sort={state.sort}
          sortHref={(sort) => list.href({ sort, page: 1 })}
          href={list.href}
          regionName={regionName}
          canChange={canChange}
          org={org}
          onDeleted={snapshots.deleted}
          onRestored={snapshots.changed}
          showRegion={false}
          empty={
            isFiltered(state) ? (
              <NoMatches label="snapshots" onClear={list.clearAll} />
            ) : (
              empty
            )
          }
        />
      )}
    </div>
  )
}
