import {
  Badge,
  Button,
  Code,
  CodeBlock,
  ConfirmationDialog,
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownMenu,
  ErrorNotice,
  Field,
  FieldGroup,
  Input,
  Label,
  PaginatedTable,
  PasswordField,
  SearchField,
  SecretReveal,
  Step,
  Steps,
  TableCell,
  TableHeader,
  TableRow,
  Text,
  toast,
  useAction,
  type BadgeColor,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { EllipsisHorizontalIcon, NoSymbolIcon } from '@heroicons/react/16/solid'
import { useState, type FormEvent } from 'react'
import { FilterBar, type FilterDef } from '../../components/filter-bar'
import { NoMatches, NoRecords, SortHeader } from '../../components/list'
import { KeyExpiryField } from '../../components/key-expiry'
import * as api from '../../lib/api'
import { serverURL } from '../../lib/commands'
import { formatDate, relativeTime } from '../../lib/format'
import { isFiltered, listQuery, type ListSpec } from '../../lib/list-query'
import type { Permissions } from '../../lib/organizations'
import { useListState } from '../../lib/use-list'
import { errorField, failureMessage, useLoad } from '../../lib/use-load'

const statusColors: Record<api.AdminKey['status'], BadgeColor> = {
  active: 'lime',
  expired: 'amber',
  revoked: 'zinc',
}

function KeyStatus({ status }: { status: api.AdminKey['status'] }) {
  return <Badge color={statusColors[status]}>{status.charAt(0).toUpperCase() + status.slice(1)}</Badge>
}

const keySpec: ListSpec = {
  sortKeys: ['created', 'name', 'expires'],
  defaultSort: '-created',
  descendingFirst: ['created', 'expires'],
  filters: { status: ['active', 'expired', 'revoked'] },
}

const keyFilters: FilterDef[] = [
  {
    key: 'status',
    label: 'Status',
    options: [
      { value: 'active', label: 'Active' },
      { value: 'expired', label: 'Expired' },
      { value: 'revoked', label: 'Revoked' },
    ],
  },
]

// An organization's management API keys (lba_…) for the Lewbox API. The
// page around it owns the "Create management key" button and opens the
// dialog.
export function AdminKeysSection({
  org,
  can,
  createOpen,
  onCreateClose,
}: {
  org: api.Organization
  can: Permissions
  createOpen: boolean
  onCreateClose: () => void
}) {
  const [revokeTarget, setRevokeTarget] = useState<api.AdminKey | null>(null)
  const list = useListState(keySpec)
  const { state } = list
  const keys = useLoad(`admin-keys:${org.slug}:${list.key()}`, () =>
    api.listAdminKeys(org.slug, {
      ...listQuery(state, keySpec),
      status: state.filters.status as api.KeyStatus[] | undefined,
    }),
  )

  return (
    <>
      <div className="space-y-4">
        <FilterBar
          filters={keyFilters}
          values={state.filters}
          onApply={list.setFilter}
          onRemove={list.removeFilter}
          onClear={list.clearFilters}
          leading={
            <SearchField
              label="Search management keys"
              value={state.q}
              onSearch={list.setSearch}
            />
          }
        />
        {keys.error.message && keys.data && (
          <ErrorNotice error={keys.error} onRetry={keys.reload} />
        )}
        {keys.error.message && !keys.data ? (
          <ErrorNotice error={keys.error} onRetry={keys.reload} />
        ) : (
          <PaginatedTable
            page={keys.data ?? undefined}
            loading={keys.loading || keys.stale}
            label="Management API keys"
            href={list.href}
            rowKey={(key) => key.id}
            head={
              <TableRow>
                <SortHeader state={state} spec={keySpec} column="name" base={list.params}>
                  Name
                </SortHeader>
                <TableHeader>Key</TableHeader>
                <TableHeader>Status</TableHeader>
                <SortHeader state={state} spec={keySpec} column="created" base={list.params}>
                  Created
                </SortHeader>
                <TableHeader>Last used</TableHeader>
                <SortHeader state={state} spec={keySpec} column="expires" base={list.params}>
                  Expires
                </SortHeader>
                {can.manage && (
                  <TableHeader className="w-0">
                    <span className="sr-only">Actions</span>
                  </TableHeader>
                )}
              </TableRow>
            }
            row={(key) => (
              <TableRow key={key.id}>
                <TableCell className="font-medium">{key.name}</TableCell>
                <TableCell>
                  <Code>
                    {key.prefix}…{key.last4}
                  </Code>
                </TableCell>
                <TableCell>
                  <KeyStatus status={key.status} />
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
                  title={formatDate(key.last_used_at)}
                >
                  {relativeTime(key.last_used_at)}
                </TableCell>
                <TableCell
                  className="text-zinc-500 dark:text-zinc-400"
                  title={key.expires_at ? formatDate(key.expires_at) : undefined}
                >
                  {key.status === 'revoked'
                    ? `Revoked ${relativeTime(key.revoked_at).toLowerCase()}`
                    : key.expires_at
                      ? relativeTime(key.expires_at)
                      : 'Never'}
                </TableCell>
                {can.manage && (
                  <TableCell className="w-0 text-right">
                    {key.status !== 'revoked' && (
                      <Dropdown>
                        <DropdownButton
                          as={Button}
                          plain
                          aria-label={`Actions for ${key.name}`}
                        >
                          <EllipsisHorizontalIcon />
                        </DropdownButton>
                        <DropdownMenu anchor="bottom end">
                          <DropdownItem
                            variant="danger"
                            onClick={() => setRevokeTarget(key)}
                          >
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
            empty={
              isFiltered(state) ? (
                <NoMatches label="management keys" onClear={list.clearAll} />
              ) : (
                <NoRecords
                  title="No management API keys"
                  description="Lets your backend call the Lewbox API as this workspace."
                />
              )
            }
          />
        )}
      </div>

      <CreateKeyDialog
        open={createOpen}
        org={org}
        onClose={onCreateClose}
        onCreated={keys.refresh}
      />
      <ConfirmationDialog
        open={revokeTarget !== null}
        onClose={() => setRevokeTarget(null)}
        title={revokeTarget ? `Revoke ${revokeTarget.name}?` : 'Revoke key?'}
        description="It stops working at once. Anything still using it gets 401 errors. This can't be undone."
        confirm="Revoke key"
        variant="danger"
        onConfirm={async () => {
          if (revokeTarget) await api.revokeAdminKey(org.slug, revokeTarget.id)
        }}
        onConfirmed={() => {
          toast.success('Key revoked.')
          keys.refresh()
        }}
      />
    </>
  )
}

function CreateKeyDialog({
  open,
  org,
  onClose,
  onCreated,
}: {
  open: boolean
  org: api.Organization
  onClose: () => void
  onCreated: () => void
}) {
  const count = useOpenCount(open)
  // Once a key exists, the dialog only closes from its Done button, so the
  // token isn't lost to a stray click outside it.
  const [created, setCreated] = useState(false)
  return (
    <Dialog open={open} onClose={created ? () => {} : onClose} size="xl">
      <CreateKey
        key={count}
        org={org}
        onClose={() => {
          setCreated(false)
          onClose()
        }}
        onCreated={() => {
          setCreated(true)
          onCreated()
        }}
      />
    </Dialog>
  )
}

function CreateKey({
  org,
  onClose,
  onCreated,
}: {
  org: api.Organization
  onClose: () => void
  onCreated: () => void
}) {
  const [name, setName] = useState('')
  const [expiresIn, setExpiresIn] = useState<api.KeyLifetime>('90d')
  const [currentPassword, setCurrentPassword] = useState('')
  const [currentError, setCurrentError] = useState('')
  const [key, setKey] = useState<api.NewAdminKey | null>(null)
  const create = useAction(
    () => api.createAdminKey(org.slug, { name, expires_in: expiresIn, current_password: currentPassword }),
    {
      onError: (failure) => {
        if (errorField(failure) !== 'current_password') return false
        setCurrentError(failureMessage(failure))
        return true
      },
      toError: (failure) => ({ message: failureMessage(failure) }),
    },
  )

  async function submit(event: FormEvent) {
    event.preventDefault()
    if (currentPassword === '') {
      setCurrentError('Enter your current password.')
      return
    }
    setCurrentError('')
    const result = await create.run()
    if (result.ok) {
      setCurrentPassword('')
      setKey(result.value)
      onCreated()
    }
  }

  if (key) {
    return (
      <>
        <DialogTitle>Copy your management key</DialogTitle>
        <DialogDescription>
          {key.name} acts for {org.name}
          {key.expires_at ? ` until ${formatDate(key.expires_at)}` : ' until you revoke it'}.
        </DialogDescription>
        <DialogBody>
          <SecretReveal secret={key.token} label="Copy management key">
            <Steps aria-label="Using the key">
              <Step title="Call the API with it">
                <CodeBlock code={`curl -H "Authorization: Bearer $LEWBOX_KEY" ${serverURL()}/v0/organization`} />
              </Step>
              <Step title="Or sign in the CLI with it">
                <CodeBlock code={`lewbox login ${serverURL()} --token`} />
              </Step>
            </Steps>
          </SecretReveal>
        </DialogBody>
        <DialogActions>
          <Button onClick={onClose}>Done</Button>
        </DialogActions>
      </>
    )
  }

  return (
    <form onSubmit={submit}>
      <DialogTitle>Create management key</DialogTitle>
      <DialogDescription>Lets your backend call the Lewbox API as {org.name}.</DialogDescription>
      <DialogBody>
        <FieldGroup>
          <ErrorNotice error={create.error} />
          <Field disabled={create.busy}>
            <Label>Name</Label>
            <Input
              required
              autoFocus
              value={name}
              placeholder="Production backend"
              onChange={(event) => setName(event.currentTarget.value)}
            />
          </Field>
          <KeyExpiryField value={expiresIn} onChange={setExpiresIn} disabled={create.busy} />
          <PasswordField
            label="Current password"
            autoComplete="current-password"
            value={currentPassword}
            onChange={setCurrentPassword}
            error={currentError}
            disabled={create.busy}
            autoFocus={false}
            required={false}
          />
        </FieldGroup>
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={create.busy}>
          Cancel
        </Button>
        <Button
          type="submit"
          loading={create.loading}
          disabled={name.trim() === ''}
          disabledReason="Fill in required fields"
        >
          Create key
        </Button>
      </DialogActions>
    </form>
  )
}
