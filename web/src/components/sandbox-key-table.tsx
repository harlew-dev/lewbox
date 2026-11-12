import {
  Badge,
  Button,
  Code,
  ConfirmationDialog,
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
  Text,
  toast,
} from '@harlew-dev/ui'
import { EllipsisHorizontalIcon, NoSymbolIcon } from '@heroicons/react/16/solid'
import { useState } from 'react'
import * as api from '../lib/api'
import { formatDate, relativeTime } from '../lib/format'

function KeyState({ apiKey }: { apiKey: api.SandboxKey }) {
  if (apiKey.revoked_at) return <Badge color="zinc">Revoked</Badge>
  if (apiKey.expired) return <Badge color="amber">Expired</Badge>
  return <Badge color="lime">Active</Badge>
}

// Sandbox API keys, one region each. Also the operator's view of a
// customer's keys.
export function SandboxKeyTable({
  keys,
  org,
  canRevoke,
  onRevoked,
}: {
  keys: api.SandboxKey[]
  org: string
  canRevoke: boolean
  onRevoked: () => void
}) {
  const [target, setTarget] = useState<api.SandboxKey | null>(null)
  return (
    <>
      <Table>
        <TableHead>
          <TableRow>
            <TableHeader>Name</TableHeader>
            <TableHeader>Region</TableHeader>
            <TableHeader>Key</TableHeader>
            <TableHeader>Status</TableHeader>
            <TableHeader>Created</TableHeader>
            <TableHeader>Expires</TableHeader>
            {canRevoke && (
              <TableHeader className="w-0">
                <span className="sr-only">Actions</span>
              </TableHeader>
            )}
          </TableRow>
        </TableHead>
        <TableBody>
          {keys.map((key) => (
            <TableRow key={key.id}>
              <TableCell className="font-medium">{key.name}</TableCell>
              <TableCell className="text-zinc-500 dark:text-zinc-400">{key.region.name}</TableCell>
              <TableCell>
                <Code>{key.token_hint}</Code>
              </TableCell>
              <TableCell>
                <KeyState apiKey={key} />
              </TableCell>
              <TableCell className="text-zinc-500 dark:text-zinc-400" title={formatDate(key.created_at)}>
                {relativeTime(key.created_at)}
                {key.created_by && <Text size="sm">by {key.created_by}</Text>}
              </TableCell>
              <TableCell
                className="text-zinc-500 dark:text-zinc-400"
                title={key.expires_at ? formatDate(key.expires_at) : undefined}
              >
                {key.revoked_at ? '—' : key.expires_at ? relativeTime(key.expires_at) : 'Never'}
              </TableCell>
              {canRevoke && (
                <TableCell className="w-0 text-right">
                  {!key.revoked_at && (
                    <Dropdown>
                      <DropdownButton as={Button} plain aria-label={`Actions for ${key.name}`}>
                        <EllipsisHorizontalIcon />
                      </DropdownButton>
                      <DropdownMenu anchor="bottom end">
                        <DropdownItem variant="danger" onClick={() => setTarget(key)}>
                          <NoSymbolIcon data-slot="icon" />
                          Revoke key
                        </DropdownItem>
                      </DropdownMenu>
                    </Dropdown>
                  )}
                </TableCell>
              )}
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <ConfirmationDialog
        open={target !== null}
        onClose={() => setTarget(null)}
        title={target ? `Revoke ${target.name}?` : 'Revoke key?'}
        description="It stops working within seconds. Sandboxes it created keep running. This can’t be undone."
        confirm="Revoke key"
        variant="danger"
        onConfirm={async () => {
          if (target) await api.revokeSandboxKey(org, target.id)
        }}
        onConfirmed={() => {
          toast.success('Key revoked.')
          onRevoked()
        }}
      />
    </>
  )
}
