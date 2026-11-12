import {
  Badge,
  Button,
  ConfirmationDialog,
  CopyText,
  ErrorNotice,
  ListSkeleton,
  Notice,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  toast,
} from '@harlew-dev/ui'
import { TrashIcon } from '@heroicons/react/16/solid'
import { useState, type ReactNode } from 'react'
import * as api from '../lib/api'
import { formatDate, formatMemory, formatNumber, relativeTime } from '../lib/format'
import { useLoad, type LoadState } from '../lib/use-load'

function snapshotSize(snapshot: api.Snapshot): string {
  return `${formatNumber(snapshot.cpu)} vCPU · ${formatMemory(snapshot.memory_mb)} · ${formatNumber(snapshot.disk_gb, 0)} GB disk`
}

// Names are unique per workspace and region, so the fleet adds the workspace.
const key = (snapshot: api.Snapshot) => `${snapshot.organization?.slug ?? ''}/${snapshot.region}/${snapshot.name}`

export interface SnapshotsState {
  load: LoadState<api.SnapshotList>
  // The snapshots, without those deleted here since the list loaded.
  list: api.Snapshot[]
  // Nodes that didn't answer, so the list may be missing some.
  partial: string[]
  deleted: (snapshot: api.Snapshot) => void
}

// Loads snapshots. An empty org is every workspace's, for an instance admin.
export function useSnapshots(org: string): SnapshotsState {
  const load = useLoad(() => api.listSnapshots(org), [org])
  // Deleted here, hidden until the list loads again: the answer it was
  // deleted from may still have them.
  const [hidden, setHidden] = useState<{
    from: api.SnapshotList | null
    keys: Set<string>
  }>({ from: null, keys: new Set() })
  const gone = hidden.from === load.data ? hidden.keys : new Set<string>()
  return {
    load,
    list: (load.data?.data ?? []).filter((snapshot) => !gone.has(key(snapshot))),
    partial: load.data?.partial_nodes ?? [],
    deleted: (snapshot) => {
      setHidden({ from: load.data, keys: new Set(gone).add(key(snapshot)) })
      load.refresh()
    },
  }
}

export function PartialNotice({ partial }: { partial: string[] }) {
  if (partial.length === 0) return null
  return (
    <Notice variant="warning" className="mb-6">
      Part of a region didn’t answer, so some snapshots may be missing.
    </Notice>
  )
}

// A snapshot table with delete (confirmed) for those who may. `fleet` adds the
// workspace and node of each snapshot, for the operator, and deletes in the
// snapshot's own workspace; otherwise `org` scopes the delete.
export function SnapshotTable({
  snapshots,
  regionName,
  canDelete,
  onDeleted,
  org = '',
  fleet = false,
  showRegion = true,
}: {
  snapshots: api.Snapshot[]
  regionName: (slug: string) => string
  canDelete: boolean
  onDeleted: (snapshot: api.Snapshot) => void
  org?: string
  fleet?: boolean
  showRegion?: boolean
}) {
  const [target, setTarget] = useState<api.Snapshot | null>(null)

  return (
    <>
      <Table>
        <TableHead>
          <TableRow>
            {fleet && <TableHeader>Workspace</TableHeader>}
            <TableHeader>Name</TableHeader>
            {showRegion && <TableHeader>Region</TableHeader>}
            {fleet && <TableHeader>Node</TableHeader>}
            <TableHeader>Source sandbox</TableHeader>
            <TableHeader>Created</TableHeader>
            <TableHeader>Size</TableHeader>
            {canDelete && (
              <TableHeader className="w-0">
                <span className="sr-only">Actions</span>
              </TableHeader>
            )}
          </TableRow>
        </TableHead>
        <TableBody>
          {snapshots.map((snapshot) => (
            <TableRow key={key(snapshot)}>
              {fleet && <TableCell className="font-medium">{snapshot.organization?.name ?? '—'}</TableCell>}
              <TableCell className="font-medium">
                {snapshot.name}
                {snapshot.push_state === 'failed' && (
                  <Badge color="red" className="ml-2">
                    Upload failed
                  </Badge>
                )}
              </TableCell>
              {showRegion && (
                <TableCell className="text-zinc-500 dark:text-zinc-400">{regionName(snapshot.region)}</TableCell>
              )}
              {fleet && <TableCell className="text-zinc-500 dark:text-zinc-400">{snapshot.node ?? '—'}</TableCell>}
              <TableCell className="max-w-56">
                {snapshot.source_sandbox_id ? (
                  <CopyText value={snapshot.source_sandbox_id} label={`Copy ${snapshot.source_sandbox_id}`} />
                ) : (
                  '—'
                )}
              </TableCell>
              <TableCell className="text-zinc-500 dark:text-zinc-400" title={formatDate(snapshot.created_at)}>
                {relativeTime(snapshot.created_at)}
              </TableCell>
              <TableCell className="whitespace-nowrap tabular-nums">{snapshotSize(snapshot)}</TableCell>
              {canDelete && (
                <TableCell className="w-0 text-right">
                  <Button plain aria-label={`Delete ${snapshot.name}`} onClick={() => setTarget(snapshot)}>
                    <TrashIcon />
                  </Button>
                </TableCell>
              )}
            </TableRow>
          ))}
        </TableBody>
      </Table>

      <ConfirmationDialog
        open={target !== null}
        onClose={() => setTarget(null)}
        title={target ? `Delete ${target.name}?` : 'Delete snapshot?'}
        description="Sandboxes can no longer start from it. This can’t be undone."
        confirm="Delete snapshot"
        variant="danger"
        onConfirm={async () => {
          if (target) await api.deleteSnapshot(fleet ? (target.organization?.slug ?? org) : org, target)
        }}
        onConfirmed={() => {
          if (target) onDeleted(target)
          toast.success('Snapshot deleted.')
        }}
      />
    </>
  )
}

// A workspace's snapshots, with delete (confirmed) for those who may.
// `empty` is shown when there are none; the portal hides the region column.
export function Snapshots({
  org,
  canDelete,
  regionName,
  empty,
  showRegion = true,
}: {
  org: string
  canDelete: boolean
  regionName: (slug: string) => string
  empty: ReactNode
  showRegion?: boolean
}) {
  const snapshots = useSnapshots(org)
  const { load } = snapshots

  if (load.loading && !load.data) return <ListSkeleton label="Loading snapshots" rows={3} />
  if (load.error.message) return <ErrorNotice error={load.error} onRetry={load.reload} />

  return (
    <>
      <PartialNotice partial={snapshots.partial} />
      {snapshots.list.length === 0 ? (
        empty
      ) : (
        <SnapshotTable
          snapshots={snapshots.list}
          regionName={regionName}
          canDelete={canDelete}
          onDeleted={snapshots.deleted}
          org={org}
          showRegion={showRegion}
        />
      )}
    </>
  )
}
