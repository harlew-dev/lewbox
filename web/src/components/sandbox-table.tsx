import {
  Badge,
  Button,
  ConfirmationDialog,
  CopyText,
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownMenu,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  toast,
  type BadgeColor,
} from '@harlew-dev/ui'
import { EllipsisHorizontalIcon, StopCircleIcon, TrashIcon } from '@heroicons/react/16/solid'
import { useState } from 'react'
import * as api from '../lib/api'
import { formatAge, formatDate, formatMemory, formatNumber, relativeTime } from '../lib/format'

const statuses: Record<string, { label: string; color: BadgeColor }> = {
  creating: { label: 'Starting', color: 'amber' },
  started: { label: 'Running', color: 'lime' },
  stopped: { label: 'Stopped', color: 'zinc' },
  destroyed: { label: 'Deleted', color: 'zinc' },
  error: { label: 'Error', color: 'red' },
}

export const sandboxStatusOptions = [
  { value: 'started', label: 'Running' },
  { value: 'stopped', label: 'Stopped' },
  { value: 'creating', label: 'Starting' },
  { value: 'error', label: 'Error' },
]

function SandboxStatus({ sandbox }: { sandbox: api.Sandbox }) {
  const status = statuses[sandbox.status] ?? { label: sandbox.status, color: 'zinc' as const }
  return (
    <Badge color={status.color} title={sandbox.status_reason || undefined}>
      {status.label}
    </Badge>
  )
}

function sandboxSize(sandbox: api.Sandbox): string {
  return `${formatNumber(sandbox.cpu)} vCPU · ${formatMemory(sandbox.memory_mb)}`
}

type Pending = { sandbox: api.Sandbox; action: 'stop' | 'delete' }

// Sandboxes with stop and delete, each confirmed. `fleet` adds the customer
// and how long each has run, for the operator. `org` scopes the actions.
export function SandboxTable({
  sandboxes,
  fleet = false,
  canChange,
  org,
  onChanged,
}: {
  sandboxes: api.Sandbox[]
  fleet?: boolean
  canChange: boolean
  org?: string
  onChanged: () => void
}) {
  const [pending, setPending] = useState<Pending | null>(null)
  const stopping = pending?.action === 'stop'

  return (
    <>
      <Table>
        <TableHead>
          <TableRow>
            {fleet && <TableHeader>Customer</TableHeader>}
            <TableHeader>Sandbox</TableHeader>
            <TableHeader>Region</TableHeader>
            <TableHeader>Status</TableHeader>
            <TableHeader>Size</TableHeader>
            <TableHeader>{fleet ? 'Running for' : 'Started'}</TableHeader>
            {canChange && (
              <TableHeader className="w-0">
                <span className="sr-only">Actions</span>
              </TableHeader>
            )}
          </TableRow>
        </TableHead>
        <TableBody>
          {sandboxes.map((sandbox) => (
            <TableRow key={`${sandbox.region}/${sandbox.id}`}>
              {fleet && (
                <TableCell className="font-medium">{sandbox.organization?.name ?? 'Unassigned'}</TableCell>
              )}
              <TableCell className="max-w-56">
                <CopyText value={sandbox.id} label={`Copy ${sandbox.id}`} />
                {sandbox.image && <div className="truncate text-xs/5 text-zinc-500 dark:text-zinc-400">{sandbox.image}</div>}
              </TableCell>
              <TableCell className="text-zinc-500 dark:text-zinc-400">{sandbox.region}</TableCell>
              <TableCell>
                <SandboxStatus sandbox={sandbox} />
              </TableCell>
              <TableCell className="whitespace-nowrap tabular-nums">{sandboxSize(sandbox)}</TableCell>
              <TableCell className="text-zinc-500 tabular-nums dark:text-zinc-400" title={formatDate(sandbox.created_at)}>
                {fleet ? (sandbox.status === 'started' ? formatAge(sandbox.created_at) : '—') : relativeTime(sandbox.created_at)}
              </TableCell>
              {canChange && (
                <TableCell className="w-0 text-right">
                  <Dropdown>
                    <DropdownButton as={Button} plain aria-label={`Actions for ${sandbox.id}`}>
                      <EllipsisHorizontalIcon />
                    </DropdownButton>
                    <DropdownMenu anchor="bottom end">
                      {sandbox.status === 'started' && (
                        <DropdownItem onClick={() => setPending({ sandbox, action: 'stop' })}>
                          <StopCircleIcon data-slot="icon" />
                          Stop
                        </DropdownItem>
                      )}
                      <DropdownItem variant="danger" onClick={() => setPending({ sandbox, action: 'delete' })}>
                        <TrashIcon data-slot="icon" />
                        Delete
                      </DropdownItem>
                    </DropdownMenu>
                  </Dropdown>
                </TableCell>
              )}
            </TableRow>
          ))}
        </TableBody>
      </Table>

      <ConfirmationDialog
        open={pending !== null}
        onClose={() => setPending(null)}
        title={`${stopping ? 'Stop' : 'Delete'} ${pending?.sandbox.id ?? 'sandbox'}?`}
        description={
          stopping
            ? 'It stops running and stops using compute. It still counts toward the limits until it is deleted.'
            : 'It is destroyed with everything in it. This can’t be undone.'
        }
        confirm={stopping ? 'Stop sandbox' : 'Delete sandbox'}
        variant={stopping ? 'default' : 'danger'}
        onConfirm={async () => {
          if (!pending) return
          if (pending.action === 'stop') await api.stopSandbox(pending.sandbox, org)
          else await api.deleteSandbox(pending.sandbox, org)
        }}
        onConfirmed={() => {
          toast.success(stopping ? 'Sandbox stopped.' : 'Sandbox deleted.')
          onChanged()
        }}
      />
    </>
  )
}
