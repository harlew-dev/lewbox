import {
  EmptyState,
  ErrorNotice,
  Listbox,
  ListboxOption,
  ListSkeleton,
  PageHeader,
  PageTabs,
  Stat,
  Subheading,
  Text,
  tabFrom,
} from '@harlew-dev/ui'
import { BarList } from '@harlew-dev/ui/chart'
import { useState } from 'react'
import { useSearchParams } from 'react-router'
import * as api from '../../lib/api'
import { ArchiveTable } from '../../components/archive-table'
import { PartialNotice, SnapshotTable, useSnapshots } from '../../components/snapshot-table'
import { formatMemory, formatNumber } from '../../lib/format'
import { useRegions } from '../../lib/regions'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'

interface Group {
  key: string
  label: string
  href: string
  count: number
  diskGB: number
}

// Sums snapshots by `by`, largest disk first.
function groupBy(snapshots: api.Snapshot[], by: (snapshot: api.Snapshot) => Omit<Group, 'count' | 'diskGB'>): Group[] {
  const groups = new Map<string, Group>()
  for (const snapshot of snapshots) {
    const group = by(snapshot)
    const sum = groups.get(group.key) ?? { ...group, count: 0, diskGB: 0 }
    sum.count += 1
    sum.diskGB += snapshot.disk_gb
    groups.set(group.key, sum)
  }
  return [...groups.values()].sort((a, b) => b.diskGB - a.diskGB || b.count - a.count || a.label.localeCompare(b.label))
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

const tabs = [
  { id: 'snapshots', label: 'Snapshots' },
  { id: 'archived', label: 'Archived' },
] as const

export function FleetSnapshotsPage() {
  const [searchParams] = useSearchParams()
  const tab = tabFrom(searchParams, tabs)
  useDocumentTitle(tab === 'archived' ? 'Archived sandboxes' : 'Snapshots')
  return tab === 'archived' ? (
    <>
      <PageHeader title="Snapshots" description="Every workspace’s snapshots, and its archived sandboxes." />
      <PageTabs label="Snapshot views" tabs={tabs} current={tab} className="mb-8" />
      <FleetArchives />
    </>
  ) : (
    <SnapshotsView />
  )
}

// Every workspace's archived sandboxes, which an instance admin may delete.
function FleetArchives() {
  const archives = useLoad(() => api.listArchives(''), [])
  const regions = useRegions()
  if (archives.loading && !archives.data) return <ListSkeleton label="Loading archived sandboxes" rows={4} />
  if (archives.error.message) return <ErrorNotice error={archives.error} onRetry={archives.reload} />
  const list = archives.data?.data ?? []
  if (list.length === 0) {
    return (
      <EmptyState
        title="No archived sandboxes"
        description="Sandboxes archived on stop, in regions that archive, show up here."
      />
    )
  }
  return (
    <ArchiveTable
      archives={list}
      canChange
      fleet
      showRegion
      regionName={regions.name}
      onChanged={archives.refresh}
    />
  )
}

function SnapshotsView() {
  const [region, setRegion] = useState('')
  const [workspace, setWorkspace] = useState('')
  const snapshots = useSnapshots('')
  const { load } = snapshots
  const regions = useRegions()

  const workspaces = groupBy(snapshots.list, (snapshot) => ({
    key: snapshot.organization?.slug ?? '',
    label: snapshot.organization?.name ?? 'Unknown',
    href: `/admin/workspaces/${encodeURIComponent(snapshot.organization?.slug ?? '')}`,
  })).sort((a, b) => a.label.localeCompare(b.label))
  const list = snapshots.list.filter(
    (snapshot) => (!region || snapshot.region === region) && (!workspace || snapshot.organization?.slug === workspace),
  )
  const diskGB = list.reduce((sum, snapshot) => sum + snapshot.disk_gb, 0)
  const memoryMB = list.reduce((sum, snapshot) => sum + snapshot.memory_mb, 0)
  const failed = list.filter((snapshot) => snapshot.push_state === 'failed').length
  const owners = new Set(list.map((snapshot) => snapshot.organization?.slug)).size
  const byNode = groupBy(list, (snapshot) => ({
    key: `${snapshot.region}/${snapshot.node ?? ''}`,
    label: `${snapshot.node ?? 'Unknown node'} (${regions.name(snapshot.region)})`,
    href: `/admin/servers/clusters/${encodeURIComponent(snapshot.region)}`,
  }))
  const byWorkspace = groupBy(list, (snapshot) => ({
    key: snapshot.organization?.slug ?? '',
    label: snapshot.organization?.name ?? 'Unknown',
    href: `/admin/workspaces/${encodeURIComponent(snapshot.organization?.slug ?? '')}`,
  }))

  return (
    <>
      <PageHeader title="Snapshots" description="Every workspace’s snapshots, on every node." />
      <PageTabs label="Snapshot views" tabs={tabs} current="snapshots" className="mb-8" />
      <div className="mb-6 flex flex-wrap gap-3">
        <Listbox aria-label="Region" value={region} onChange={setRegion} className="sm:w-48">
          <ListboxOption value="">All regions</ListboxOption>
          {regions.list.map((option) => (
            <ListboxOption key={option.slug} value={option.slug}>
              {option.name}
            </ListboxOption>
          ))}
        </Listbox>
        <Listbox aria-label="Workspace" value={workspace} onChange={setWorkspace} className="sm:w-48">
          <ListboxOption value="">All workspaces</ListboxOption>
          {workspaces.map((option) => (
            <ListboxOption key={option.key} value={option.key}>
              {option.label}
            </ListboxOption>
          ))}
        </Listbox>
      </div>

      {load.loading && !load.data ? (
        <ListSkeleton label="Loading snapshots" rows={4} />
      ) : load.error.message ? (
        <ErrorNotice error={load.error} onRetry={load.reload} />
      ) : (
        <>
          <PartialNotice partial={snapshots.partial} />
          <div className="grid gap-8 sm:grid-cols-2 xl:grid-cols-4">
            <Stat title="Snapshots" value={formatNumber(list.length, 0)}>
              <span>across {plural(owners, 'workspace')}</span>
            </Stat>
            <Stat title="Disk" value={`${formatNumber(diskGB, 0)} GB`}>
              <span>of sandbox disk captured</span>
            </Stat>
            <Stat title="Memory" value={formatMemory(memoryMB)}>
              <span>of sandbox memory captured</span>
            </Stat>
            <Stat title="Failed uploads" value={formatNumber(failed, 0)} level={failed > 0 ? 'warn' : undefined}>
              <span>not pushed to the registry</span>
            </Stat>
          </div>
          <Text className="mt-4">
            AerolVM doesn’t report how much space a snapshot takes, so these totals add up the disk and memory size of
            the sandbox each snapshot was taken from.
          </Text>

          {list.length === 0 ? (
            <div className="mt-10">
              <EmptyState
                title="No snapshots"
                description={
                  region || workspace ? 'None match these filters.' : 'Snapshots workspaces take show up here.'
                }
              />
            </div>
          ) : (
            <>
              <div className="mt-14 grid gap-10 lg:grid-cols-2">
                <GroupList title="By node" groups={byNode} />
                <GroupList title="By workspace" groups={byWorkspace} />
              </div>
              <Subheading className="mt-14 mb-4">All snapshots</Subheading>
              <SnapshotTable snapshots={list} regionName={regions.name} canDelete fleet onDeleted={snapshots.deleted} />
            </>
          )}
        </>
      )}
    </>
  )
}
