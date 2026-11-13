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
  EmptyState,
  ErrorNotice,
  Field,
  FieldGroup,
  Input,
  Label,
  ListSkeleton,
  SecretReveal,
  Step,
  Steps,
  Table,
  TableBody,
  TableCell,
  TableHead,
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
import { KeyExpiryField } from '../../components/key-expiry'
import * as api from '../../lib/api'
import { serverURL } from '../../lib/commands'
import { formatDate, relativeTime } from '../../lib/format'
import type { Permissions } from '../../lib/organizations'
import { failureMessage, useLoad } from '../../lib/use-load'

const statusColors: Record<api.AdminKey['status'], BadgeColor> = {
  active: 'lime',
  expired: 'amber',
  revoked: 'zinc',
}

function KeyStatus({ status }: { status: api.AdminKey['status'] }) {
  return <Badge color={statusColors[status]}>{status.charAt(0).toUpperCase() + status.slice(1)}</Badge>
}

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
  const keys = useLoad(() => api.listAdminKeys(org.slug), [org.slug])
  const list = keys.data?.data ?? []

  return (
    <>
      {keys.loading && !keys.data ? (
        <ListSkeleton label="Loading API keys" rows={3} />
      ) : keys.error.message ? (
        <ErrorNotice error={keys.error} onRetry={keys.reload} />
      ) : list.length === 0 ? (
        <EmptyState
          title="No management API keys"
          description="Lets your backend call the Lewbox API as this workspace."
        />
      ) : (
        <Table>
          <TableHead>
            <TableRow>
              <TableHeader>Name</TableHeader>
              <TableHeader>Key</TableHeader>
              <TableHeader>Status</TableHeader>
              <TableHeader>Created</TableHeader>
              <TableHeader>Last used</TableHeader>
              <TableHeader>Expires</TableHeader>
              {can.manage && (
                <TableHeader className="w-0">
                  <span className="sr-only">Actions</span>
                </TableHeader>
              )}
            </TableRow>
          </TableHead>
          <TableBody>
            {list.map((key) => (
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
                <TableCell className="text-zinc-500 dark:text-zinc-400" title={formatDate(key.created_at)}>
                  {relativeTime(key.created_at)}
                  {key.created_by && <Text size="sm">by {key.created_by}</Text>}
                </TableCell>
                <TableCell className="text-zinc-500 dark:text-zinc-400" title={formatDate(key.last_used_at)}>
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
                        <DropdownButton as={Button} plain aria-label={`Actions for ${key.name}`}>
                          <EllipsisHorizontalIcon />
                        </DropdownButton>
                        <DropdownMenu anchor="bottom end">
                          <DropdownItem variant="danger" onClick={() => setRevokeTarget(key)}>
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
      )}

      <CreateKeyDialog open={createOpen} org={org} onClose={onCreateClose} onCreated={keys.refresh} />
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
  const [key, setKey] = useState<api.NewAdminKey | null>(null)
  const create = useAction(() => api.createAdminKey(org.slug, { name, expires_in: expiresIn }), {
    toError: (failure) => ({ message: failureMessage(failure) }),
  })

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await create.run()
    if (result.ok) {
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
