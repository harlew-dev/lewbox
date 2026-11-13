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
import { useState } from 'react'
import * as api from '../lib/api'
import { formatDate, formatMemory, formatNumber, relativeTime } from '../lib/format'

function archiveSize(archive: api.Archive): string {
  return `${formatNumber(archive.cpu)} vCPU · ${formatMemory(archive.memory_mb)} · ${formatNumber(archive.disk_gb, 0)} GB disk`
}

function ArchiveStatus({ archive }: { archive: api.Archive }) {
  if (archive.status === 'pending' || archive.status === 'pushing') {
    return <Badge color="amber">Archiving…</Badge>
  }
  if (archive.status === 'failed') {
    return (
      <span className="whitespace-normal">
        <Badge color="red">Failed</Badge>
        {archive.error && <span className="ml-2 text-sm text-zinc-500 dark:text-zinc-400">{archive.error}</span>}
      </span>
    )
  }
  return <Badge color="lime">Archived</Badge>
}

// A workspace's archives with Restore and Delete (both confirmed) for those
// who may; viewers see the list only. `fleet` is the operator's view of every
// workspace's archives: it adds the workspace and offers Delete only.
export function ArchiveTable({
  archives,
  canChange,
  org = '',
  fleet = false,
  regionName,
  showRegion = false,
  onChanged,
}: {
  archives: api.Archive[]
  canChange: boolean
  org?: string
  fleet?: boolean
  regionName: (slug: string) => string
  showRegion?: boolean
  // Called after a restore or a delete, to load the list again.
  onChanged: () => void
}) {
  const [restoring, setRestoring] = useState<api.Archive | null>(null)
  const [deleting, setDeleting] = useState<api.Archive | null>(null)
  const [restored, setRestored] = useState<api.RestoredArchive | null>(null)
  const actions = canChange || fleet
  const label = (archive: api.Archive) => archive.sandbox_name || archive.sandbox_id

  return (
    <>
      <Table>
        <TableHead>
          <TableRow>
            {fleet && <TableHeader>Workspace</TableHeader>}
            <TableHeader>Sandbox</TableHeader>
            {showRegion && <TableHeader>Region</TableHeader>}
            <TableHeader>Archived</TableHeader>
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
          {archives.map((archive) => {
            const when = archive.archived_at ?? archive.created_at
            return (
              <TableRow key={archive.id}>
                {fleet && <TableCell className="font-medium">{archive.org ?? '—'}</TableCell>}
                <TableCell className="font-medium">{label(archive)}</TableCell>
                {showRegion && (
                  <TableCell className="text-zinc-500 dark:text-zinc-400">{regionName(archive.region)}</TableCell>
                )}
                <TableCell className="text-zinc-500 dark:text-zinc-400" title={formatDate(when)}>
                  {relativeTime(when)}
                </TableCell>
                <TableCell className="whitespace-nowrap tabular-nums">{archiveSize(archive)}</TableCell>
                <TableCell>
                  <ArchiveStatus archive={archive} />
                </TableCell>
                <TableCell className="max-w-56">
                  {archive.last_restored_sandbox_id ? (
                    <CopyText
                      value={archive.last_restored_sandbox_id}
                      label={`Copy ${archive.last_restored_sandbox_id}`}
                    />
                  ) : (
                    '—'
                  )}
                </TableCell>
                {actions && (
                  <TableCell className="w-0 text-right whitespace-nowrap">
                    {!fleet && (
                      <Button
                        plain
                        aria-label={`Restore ${label(archive)}`}
                        disabled={archive.status !== 'archived'}
                        disabledReason="Only a finished archive can be restored"
                        onClick={() => setRestoring(archive)}
                      >
                        <ArrowUturnLeftIcon />
                      </Button>
                    )}
                    <Button plain aria-label={`Delete ${label(archive)}`} onClick={() => setDeleting(archive)}>
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
        title={restoring ? `Restore ${label(restoring)}?` : 'Restore sandbox?'}
        description="This starts a new sandbox with a new ID. It keeps the same name, tags and size, but environment variables aren’t kept: pass them again. The archive stays, so you can restore it again."
        confirm="Restore sandbox"
        onConfirm={async () => {
          if (restoring) setRestored(await api.restoreArchive(org, restoring.id))
        }}
        onConfirmed={onChanged}
      />
      <ConfirmationDialog
        open={deleting !== null}
        onClose={() => setDeleting(null)}
        title={deleting ? `Delete the archive of ${label(deleting)}?` : 'Delete archive?'}
        description="The stored sandbox is removed and can no longer be restored. This can’t be undone."
        confirm="Delete archive"
        variant="danger"
        onConfirm={async () => {
          if (deleting) await api.deleteArchive(fleet ? (deleting.org ?? org) : org, deleting.id)
        }}
        onConfirmed={() => {
          toast.success('Archive deleted.')
          onChanged()
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
