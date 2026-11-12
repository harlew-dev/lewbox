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
  type BadgeColor,
} from '@harlew-dev/ui'
import { TrashIcon } from '@heroicons/react/16/solid'
import { useState, type ReactNode } from 'react'
import * as api from '../lib/api'
import { formatDate, formatMemory, formatNumber, relativeTime } from '../lib/format'
import { useLoad } from '../lib/use-load'

const pushStates: Record<string, { label: string; color: BadgeColor }> = {
  active: { label: 'Active', color: 'lime' },
  pushed: { label: 'Pushed', color: 'lime' },
  pending: { label: 'Pending', color: 'amber' },
  pushing: { label: 'Pushing', color: 'amber' },
  failed: { label: 'Failed', color: 'red' },
  error: { label: 'Failed', color: 'red' },
}

function PushState({ snapshot }: { snapshot: api.Snapshot }) {
  if (!snapshot.push_state) return <span className="text-zinc-500 dark:text-zinc-400">—</span>
  const state = pushStates[snapshot.push_state] ?? { label: snapshot.push_state, color: 'zinc' as const }
  return (
    <Badge color={state.color} title={snapshot.push_error || undefined}>
      {state.label}
    </Badge>
  )
}

function snapshotSize(snapshot: api.Snapshot): string {
  return `${formatNumber(snapshot.cpu)} vCPU · ${formatMemory(snapshot.memory_mb)} · ${formatNumber(snapshot.disk_gb, 0)} GB disk`
}

// A workspace's snapshots in every region it has a plan in, with delete
// (confirmed) for those who may. `empty` is shown when there are none.
export function Snapshots({ org, canDelete, empty }: { org: string; canDelete: boolean; empty: ReactNode }) {
  const snapshots = useLoad(() => api.listSnapshots(org), [org])
  const [target, setTarget] = useState<api.Snapshot | null>(null)
  // Deleted here, hidden even if a cached list still has them.
  const [deleted, setDeleted] = useState<Set<string>>(() => new Set())
  const key = (snapshot: api.Snapshot) => `${snapshot.region}/${snapshot.name}`
  const list = (snapshots.data?.data ?? []).filter((snapshot) => !deleted.has(key(snapshot)))
  const partial = snapshots.data?.partial_nodes ?? []

  if (snapshots.loading && !snapshots.data) return <ListSkeleton label="Loading snapshots" rows={3} />
  if (snapshots.error.message) return <ErrorNotice error={snapshots.error} onRetry={snapshots.reload} />

  return (
    <>
      {partial.length > 0 && (
        <Notice variant="warning" className="mb-6">
          Part of a region didn’t answer, so some snapshots may be missing.
        </Notice>
      )}
      {list.length === 0 ? (
        empty
      ) : (
        <Table>
          <TableHead>
            <TableRow>
              <TableHeader>Region</TableHeader>
              <TableHeader>Name</TableHeader>
              <TableHeader>Source sandbox</TableHeader>
              <TableHeader>Created</TableHeader>
              <TableHeader>Size</TableHeader>
              <TableHeader>Push state</TableHeader>
              {canDelete && (
                <TableHeader className="w-0">
                  <span className="sr-only">Actions</span>
                </TableHeader>
              )}
            </TableRow>
          </TableHead>
          <TableBody>
            {list.map((snapshot) => (
              <TableRow key={key(snapshot)}>
                <TableCell className="text-zinc-500 dark:text-zinc-400">{snapshot.region}</TableCell>
                <TableCell className="font-medium">{snapshot.name}</TableCell>
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
                <TableCell>
                  <PushState snapshot={snapshot} />
                </TableCell>
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
      )}

      <ConfirmationDialog
        open={target !== null}
        onClose={() => setTarget(null)}
        title={target ? `Delete ${target.name}?` : 'Delete snapshot?'}
        description="Sandboxes can no longer start from it. This can’t be undone."
        confirm="Delete snapshot"
        variant="danger"
        onConfirm={async () => {
          if (target) await api.deleteSnapshot(org, target)
        }}
        onConfirmed={() => {
          if (target) setDeleted((before) => new Set(before).add(key(target)))
          toast.success('Snapshot deleted.')
          snapshots.refresh()
        }}
      />
    </>
  )
}
