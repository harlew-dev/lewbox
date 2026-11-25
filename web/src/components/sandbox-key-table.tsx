import {
  Badge,
  Button,
  Code,
  ConfirmationDialog,
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownMenu,
  PaginatedTable,
  TableCell,
  TableHeader,
  TableRow,
  Text,
  toast,
} from '@harlew-dev/ui'
import { EllipsisHorizontalIcon, NoSymbolIcon } from '@heroicons/react/16/solid'
import { useState, type ReactNode } from 'react'
import * as api from '../lib/api'
import { formatDate, relativeTime } from '../lib/format'
import { sortDirection, toggleSort } from '../lib/list-query'

function KeyState({ apiKey }: { apiKey: api.SandboxKey }) {
  if (apiKey.revoked_at) return <Badge color="zinc">Revoked</Badge>
  if (apiKey.expired) return <Badge color="amber">Expired</Badge>
  return <Badge color="lime">Active</Badge>
}

// Sandbox API keys, one region each. Also the operator's view of a
// customer's keys.
export function SandboxKeyTable({
  page,
  loading,
  empty,
  sort,
  sortHref,
  href,
  org,
  canRevoke,
  onRevoked,
  showRegion = true,
}: {
  page: api.SandboxKeyList | undefined
  loading: boolean
  empty: ReactNode
  sort: string
  sortHref: (sort: string) => string
  href: (page: number) => string
  org: string
  canRevoke: boolean
  onRevoked: () => void
  showRegion?: boolean
}) {
  const [target, setTarget] = useState<api.SandboxKey | null>(null)

  function Sorted({
    column,
    first = 'ascending',
    className,
    children,
  }: {
    column: string
    first?: 'ascending' | 'descending'
    className?: string
    children: ReactNode
  }) {
    return (
      <TableHeader
        href={sortHref(toggleSort(sort, column, first))}
        sort={sortDirection(sort, column)}
        className={className}
      >
        {children}
      </TableHeader>
    )
  }

  return (
    <>
      <PaginatedTable
        page={page}
        loading={loading}
        label="API keys"
        href={href}
        empty={empty}
        rowKey={(key) => key.id}
        head={
          <TableRow>
            <Sorted column="name">Name</Sorted>
            {showRegion && <TableHeader>Region</TableHeader>}
            <TableHeader>Endpoint URL</TableHeader>
            <TableHeader>Key</TableHeader>
            <TableHeader>Status</TableHeader>
            <Sorted column="created" first="descending">
              Created
            </Sorted>
            <Sorted column="expires" first="descending">
              Expires
            </Sorted>
            {canRevoke && (
              <TableHeader className="w-0">
                <span className="sr-only">Actions</span>
              </TableHeader>
            )}
          </TableRow>
        }
        row={(key) => (
          <TableRow key={key.id}>
            <TableCell className="font-medium">{key.name}</TableCell>
            {showRegion && (
              <TableCell className="text-zinc-500 dark:text-zinc-400">
                {key.region.name}
              </TableCell>
            )}
            <TableCell>
              <Code>{key.region.endpoint_url}</Code>
            </TableCell>
            <TableCell>
              <Code>{key.token_hint}</Code>
            </TableCell>
            <TableCell>
              <KeyState apiKey={key} />
            </TableCell>
            <TableCell
              className="text-zinc-500 dark:text-zinc-400"
              title={formatDate(key.created_at)}
            >
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
        )}
      />
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
