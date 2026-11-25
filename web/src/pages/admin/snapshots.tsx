import {
  ErrorNotice,
  PageHeader,
  SearchField,
  Stat,
  Subheading,
  Text,
} from '@harlew-dev/ui'
import { BarList } from '@harlew-dev/ui/chart'
import * as api from '../../lib/api'
import { FilterBar, type FilterDef } from '../../components/filter-bar'
import { NoMatches, NoRecords } from '../../components/list'
import {
  PartialNotice,
  SnapshotTable,
  snapshotPage,
  useSnapshots,
} from '../../components/snapshot-table'
import { formatMemory, formatNumber } from '../../lib/format'
import { isFiltered, listQuery, type ListSpec } from '../../lib/list-query'
import { loadOrganizationOptions, useOptionLabels } from '../../lib/list-options'
import { useRegions } from '../../lib/regions'
import { routes } from '../../lib/routes'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useListState } from '../../lib/use-list'

interface Group {
  key: string
  label: string
  href: string
  count: number
  diskGB: number
}

function plural(count: number, word: string): string {
  return `${formatNumber(count, 0)} ${word}${count === 1 ? '' : 's'}`
}

function GroupList({ title, groups }: { title: string; groups: Group[] }) {
  const top = groups.slice(0, 8)
  return (
    <div>
      <Subheading className="mb-4">{title}</Subheading>
      <BarList
        label={`${title}, in GB of disk`}
        total={top.reduce((sum, group) => sum + group.diskGB, 0)}
        items={top.map((group) => ({
          key: group.key,
          label: `${group.label} · ${plural(group.count, 'snapshot')}`,
          value: group.diskGB,
          href: group.href,
        }))}
      />
    </div>
  )
}

const spec: ListSpec = {
  sortKeys: ['created', 'name', 'size'],
  defaultSort: '-created',
  descendingFirst: ['created', 'size'],
  filters: {
    kind: ['snapshot', 'archive'],
    region: null,
    organization: null,
    status: ['pending', 'pushing', 'active', 'error'],
  },
}

const statusOptions = [
  { value: 'pending', label: 'Pending' },
  { value: 'pushing', label: 'Pushing' },
  { value: 'active', label: 'Active' },
  { value: 'error', label: 'Error' },
]

export function FleetSnapshotsPage() {
  useDocumentTitle('Snapshots')
  const list = useListState(spec)
  const { state } = list
  const regions = useRegions()
  const organizations = useOptionLabels(
    state.filters.organization ?? [],
    loadOrganizationOptions,
  )
  const snapshots = useSnapshots(
    '',
    {
      ...listQuery(state, spec),
      kind: state.filters.kind as api.SnapshotKind[] | undefined,
      region: state.filters.region,
      organization: state.filters.organization,
      status: state.filters.status as api.SnapshotStatus[] | undefined,
      summary: 'include',
    },
    list.key(),
  )
  const { load, summary } = snapshots

  const filters: FilterDef[] = [
    {
      key: 'kind',
      label: 'Kind',
      options: [
        { value: 'snapshot', label: 'Snapshots' },
        { value: 'archive', label: 'Archived sandboxes' },
      ],
    },
    { key: 'status', label: 'Status', options: statusOptions },
    {
      key: 'region',
      label: 'Region',
      options: regions.list.map((region) => ({ value: region.slug, label: region.name })),
    },
    {
      key: 'organization',
      label: 'Workspace',
      load: loadOrganizationOptions,
      selectedLabels: organizations,
    },
  ]

  const byNode: Group[] = (summary?.by_node ?? [])
    .map((group) => ({
      key: `${group.region}/${group.kind}/${group.node ?? ''}`,
      label:
        group.kind === 'archive'
          ? `Archived to storage (${regions.name(group.region)})`
          : `${group.node ?? 'Unknown node'} (${regions.name(group.region)})`,
      href: routes.admin.cluster(group.region),
      count: group.count,
      diskGB: group.disk_gb,
    }))
    .sort((a, b) => b.diskGB - a.diskGB || b.count - a.count || a.label.localeCompare(b.label))
  const byWorkspace: Group[] = (summary?.by_workspace ?? [])
    .map((group) => ({
      key: group.slug,
      label: group.name,
      href: routes.admin.workspace(group.slug),
      count: group.count,
      diskGB: group.disk_gb,
    }))
    .sort((a, b) => a.label.localeCompare(b.label))

  return (
    <>
      <PageHeader
        title="Snapshots"
        description="Every workspace’s snapshots and archived sandboxes, on every node and in every bucket."
      />
      <div className="space-y-4">
        <FilterBar
          filters={filters}
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

        {load.error.message && load.data && (
          <ErrorNotice error={load.error} onRetry={load.reload} />
        )}
        {load.error.message && !load.data ? (
          <ErrorNotice error={load.error} onRetry={load.reload} />
        ) : (
          <>
            <PartialNotice partial={snapshots.partial} />
            {summary && (
              <>
                <div className="grid gap-8 sm:grid-cols-2 xl:grid-cols-4">
                  <Stat title="Snapshots" value={formatNumber(summary.count, 0)}>
                    <span>across {plural(summary.workspaces, 'workspace')}</span>
                  </Stat>
                  <Stat title="Disk" value={`${formatNumber(summary.disk_gb, 0)} GB`}>
                    <span>of sandbox disk captured</span>
                  </Stat>
                  <Stat title="Memory" value={formatMemory(summary.memory_mb)}>
                    <span>of sandbox memory captured</span>
                  </Stat>
                  <Stat
                    title="Failed uploads"
                    value={formatNumber(summary.failed, 0)}
                    level={summary.failed > 0 ? 'warn' : undefined}
                  >
                    <span>not pushed to the registry</span>
                  </Stat>
                </div>
                <Text className="mt-4">
                  AerolVM doesn’t report how much space a snapshot takes, so these totals add up
                  the disk and memory size of the sandbox each snapshot was taken from.
                </Text>

                {summary.count > 0 && (
                  <div className="mt-14 grid gap-10 lg:grid-cols-2">
                    <GroupList title="By node" groups={byNode} />
                    <GroupList title="By workspace" groups={byWorkspace} />
                  </div>
                )}
              </>
            )}

            <Subheading className="mt-14 mb-4">All snapshots and archives</Subheading>
            <SnapshotTable
              page={snapshotPage(snapshots)}
              loading={load.loading || load.stale}
              sort={state.sort}
              sortHref={(sort) => list.href({ sort, page: 1 })}
              href={list.href}
              regionName={regions.name}
              canChange
              fleet
              onDeleted={snapshots.deleted}
              onRestored={snapshots.changed}
              empty={
                isFiltered(state) ? (
                  <NoMatches label="snapshots" onClear={list.clearAll} />
                ) : (
                  <NoRecords
                    title="No snapshots"
                    description="Snapshots workspaces take and sandboxes archived on stop show up here."
                  />
                )
              }
            />
          </>
        )}
      </div>
    </>
  )
}
