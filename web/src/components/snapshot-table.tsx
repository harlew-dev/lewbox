import {
  Badge,
  Button,
  ConfirmationDialog,
  CopyText,
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
  ErrorNotice,
  ListSkeleton,
  Notice,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Text,
  toast,
} from '@harlew-dev/ui'
import { ArrowUturnLeftIcon, TrashIcon } from '@heroicons/react/16/solid'
import { useState, type ReactNode } from 'react'
import * as api from '../lib/api'
import { formatDate, formatMemory, formatNumber, relativeTime } from '../lib/format'
import { useLoad, type LoadState } from '../lib/use-load'

function snapshotSize(snapshot: api.Snapshot): string {
  return `${formatNumber(snapshot.cpu)} vCPU · ${formatMemory(snapshot.memory_mb)} · ${formatNumber(snapshot.disk_gb, 0)} GB disk`
}

// A snapshot's name is unique per workspace and region, while an archive has a
// unique ID.
const key = (snapshot: api.Snapshot) =>
  snapshot.kind === 'archive'
    ? `archive/${snapshot.id}`
    : `${snapshot.organization?.slug ?? ''}/${snapshot.region}/${snapshot.name}`

// An archive reads best by the sandbox it came from.
const archiveLabel = (snapshot: api.Snapshot) =>
  snapshot.sandbox_name || snapshot.source_sandbox_id || snapshot.name

const rowLabel = (snapshot: api.Snapshot) =>
  snapshot.kind === 'archive' ? archiveLabel(snapshot) : snapshot.name

// The workspace a row's action acts on: the owner's for the operator, the
// portal's own otherwise.
const actionOrg = (snapshot: api.Snapshot, fleet: boolean, org: string) =>
  fleet ? (snapshot.organization?.slug ?? org) : org

export interface SnapshotsState {
  load: LoadState<api.SnapshotList>
  // The snapshots and archives, without those deleted here since the list
  // loaded.
  list: api.Snapshot[]
  // Nodes that didn't answer, so the list may be missing some snapshots.
  partial: string[]
  deleted: (snapshot: api.Snapshot) => void
  // Reload after a restore, which changes a row but deletes none.
  changed: () => void
}

// Loads snapshots and archived sandboxes. An empty org is every workspace's,
// for an instance admin.
export function useSnapshots(org: string): SnapshotsState {
  const load = useLoad(`snapshots:${org}`, () => api.listSnapshots(org))
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
    changed: load.refresh,
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

function SnapshotStatus({ snapshot }: { snapshot: api.Snapshot }) {
  if (!snapshot.status) return <span className="text-zinc-500 dark:text-zinc-400">—</span>
  switch (snapshot.status) {
    case 'pending':
      return <Badge color="amber">Pending</Badge>
    case 'pushing':
      return <Badge color="amber">Pushing</Badge>
    case 'active':
      return <Badge color="lime">Active</Badge>
    case 'error':
      return (
        <span className="whitespace-normal">
          <Badge color="red">Error</Badge>
          {snapshot.error && (
            <span className="ml-2 text-sm text-zinc-500 dark:text-zinc-400">{snapshot.error}</span>
          )}
        </span>
      )
    default:
      return <span>{snapshot.status}</span>
  }
}

// One table for snapshots and archived sandboxes, with Delete (confirmed) for
// those who may and Restore for an active archive. `fleet` adds the workspace
// and node of each row, for the operator, and acts in the row's own
// workspace; otherwise `org` scopes the action.
export function SnapshotTable({
  snapshots,
  regionName,
  canChange,
  onDeleted,
  onRestored,
  org = '',
  fleet = false,
  showRegion = true,
}: {
  snapshots: api.Snapshot[]
  regionName: (slug: string) => string
  canChange: boolean
  onDeleted: (snapshot: api.Snapshot) => void
  onRestored?: () => void
  org?: string
  fleet?: boolean
  showRegion?: boolean
}) {
  const [restoring, setRestoring] = useState<api.Snapshot | null>(null)
  const [deleting, setDeleting] = useState<api.Snapshot | null>(null)
  const [restored, setRestored] = useState<api.RestoredArchive | null>(null)
  const actions = canChange || fleet
  const deletingArchive = deleting?.kind === 'archive'

  return (
    <>
      <Table>
        <TableHead>
          <TableRow>
            {fleet && <TableHeader>Workspace</TableHeader>}
            <TableHeader>Kind</TableHeader>
            <TableHeader>Name</TableHeader>
            {showRegion && <TableHeader>Region</TableHeader>}
            {fleet && <TableHeader>Node</TableHeader>}
            <TableHeader>Source sandbox</TableHeader>
            <TableHeader>Created</TableHeader>
            <TableHeader>Size</TableHeader>
            <TableHeader>Status</TableHeader>
            <TableHeader>Last restored as</TableHeader>
            {actions && (
              <TableHeader className="w-0">
                <span className="sr-only">Actions</span>
              </TableHeader>
            )}
          </TableRow>
        </TableHead>
        <TableBody>
          {snapshots.map((snapshot) => {
            const when =
              snapshot.kind === 'archive' ? (snapshot.archived_at ?? snapshot.created_at) : snapshot.created_at
            return (
              <TableRow key={key(snapshot)}>
                {fleet && <TableCell className="font-medium">{snapshot.organization?.name ?? '—'}</TableCell>}
                <TableCell className="whitespace-nowrap">
                  {snapshot.kind === 'archive' ? 'Archived sandbox' : 'Snapshot'}
                </TableCell>
                <TableCell className="font-medium">{rowLabel(snapshot)}</TableCell>
                {showRegion && (
                  <TableCell className="text-zinc-500 dark:text-zinc-400">{regionName(snapshot.region)}</TableCell>
                )}
                {fleet && (
                  <TableCell className="text-zinc-500 dark:text-zinc-400">
                    {snapshot.kind === 'archive' ? '—' : (snapshot.node ?? '—')}
                  </TableCell>
                )}
                <TableCell className="max-w-56">
                  {snapshot.source_sandbox_id ? (
                    <CopyText
                      value={snapshot.source_sandbox_id}
                      label={`Copy ${snapshot.source_sandbox_id}`}
                    />
                  ) : (
                    '—'
                  )}
                </TableCell>
                <TableCell className="text-zinc-500 dark:text-zinc-400" title={formatDate(when)}>
                  {relativeTime(when)}
                </TableCell>
                <TableCell className="whitespace-nowrap tabular-nums">{snapshotSize(snapshot)}</TableCell>
                <TableCell>
                  <SnapshotStatus snapshot={snapshot} />
                </TableCell>
                <TableCell className="max-w-56">
                  {snapshot.last_restored_sandbox_id ? (
                    <CopyText
                      value={snapshot.last_restored_sandbox_id}
                      label={`Copy ${snapshot.last_restored_sandbox_id}`}
                    />
                  ) : (
                    '—'
                  )}
                </TableCell>
                {actions && (
                  <TableCell className="w-0 text-right whitespace-nowrap">
                    {!fleet && canChange && snapshot.kind === 'archive' && snapshot.status === 'active' && (
                      <Button
                        plain
                        aria-label={`Restore ${archiveLabel(snapshot)}`}
                        onClick={() => setRestoring(snapshot)}
                      >
                        <ArrowUturnLeftIcon />
                      </Button>
                    )}
                    <Button plain aria-label={`Delete ${rowLabel(snapshot)}`} onClick={() => setDeleting(snapshot)}>
                      <TrashIcon />
                    </Button>
                  </TableCell>
                )}
              </TableRow>
            )
          })}
        </TableBody>
      </Table>

      <ConfirmationDialog
        open={restoring !== null}
        onClose={() => setRestoring(null)}
        title={restoring ? `Restore ${archiveLabel(restoring)}?` : 'Restore sandbox?'}
        description="This starts a new sandbox with a new ID. It keeps the same name, tags and size, but environment variables aren’t kept: pass them again. The archive stays, so you can restore it again."
        confirm="Restore sandbox"
        onConfirm={async () => {
          if (restoring) {
            setRestored(await api.restoreArchive(actionOrg(restoring, fleet, org), restoring.id))
          }
        }}
        onConfirmed={() => onRestored?.()}
      />
      <ConfirmationDialog
        open={deleting !== null}
        onClose={() => setDeleting(null)}
        title={
          deleting
            ? deletingArchive
              ? `Delete the archive of ${archiveLabel(deleting)}?`
              : `Delete ${deleting.name}?`
            : 'Delete snapshot?'
        }
        description={
          deletingArchive
            ? 'The stored sandbox is removed and can no longer be restored. This can’t be undone.'
            : 'Sandboxes can no longer start from it. This can’t be undone.'
        }
        confirm={deletingArchive ? 'Delete archive' : 'Delete snapshot'}
        variant="danger"
        onConfirm={async () => {
          if (!deleting) return
          const orgRef = actionOrg(deleting, fleet, org)
          if (deleting.kind === 'archive') await api.deleteArchive(orgRef, deleting.id)
          else await api.deleteSnapshot(orgRef, deleting)
        }}
        onConfirmed={() => {
          if (deletingArchive) toast.success('Archive deleted.')
          else toast.success('Snapshot deleted.')
          if (deleting) onDeleted(deleting)
        }}
      />
      <Dialog open={restored !== null} onClose={() => setRestored(null)}>
        <DialogTitle>Sandbox restored</DialogTitle>
        <DialogDescription>
          {restored ? `${restored.sandbox.name || 'The sandbox'} is starting as a new sandbox.` : ''}
        </DialogDescription>
        <DialogBody>
          <Text className="mb-2">New sandbox ID</Text>
          {restored && <CopyText value={restored.sandbox.id} label={`Copy ${restored.sandbox.id}`} />}
        </DialogBody>
        <DialogActions>
          <Button onClick={() => setRestored(null)}>Done</Button>
        </DialogActions>
      </Dialog>
    </>
  )
}

// A workspace's snapshots and archived sandboxes, with actions for those who
// may. `empty` is shown when there are none; the portal hides the region
// column.
export function Snapshots({
  org,
  canChange,
  regionName,
  empty,
  showRegion = true,
}: {
  org: string
  canChange: boolean
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
          canChange={canChange}
          onDeleted={snapshots.deleted}
          onRestored={snapshots.changed}
          org={org}
          showRegion={showRegion}
        />
      )}
    </>
  )
}
