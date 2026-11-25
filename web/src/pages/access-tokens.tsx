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
  toast,
  useAction,
  type BadgeColor,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { EllipsisHorizontalIcon, NoSymbolIcon } from '@heroicons/react/16/solid'
import { useState, type FormEvent } from 'react'
import { FilterBar, type FilterDef } from '../components/filter-bar'
import { KeyExpiryField } from '../components/key-expiry'
import { NoMatches, NoRecords, SortHeader } from '../components/list'
import * as api from '../lib/api'
import { serverURL } from '../lib/commands'
import { formatDate, relativeTime } from '../lib/format'
import { isFiltered, listQuery, type ListSpec } from '../lib/list-query'
import { useListState } from '../lib/use-list'
import { errorField, failureMessage, useLoad } from '../lib/use-load'

const statusColors: Record<api.AccessToken['status'], BadgeColor> = {
  active: 'lime',
  expired: 'amber',
  revoked: 'zinc',
}

function TokenStatus({ status }: { status: api.AccessToken['status'] }) {
  return <Badge color={statusColors[status]}>{status.charAt(0).toUpperCase() + status.slice(1)}</Badge>
}

const tokenSpec: ListSpec = {
  sortKeys: ['created', 'name', 'expires'],
  defaultSort: '-created',
  descendingFirst: ['created', 'expires'],
  filters: { status: ['active', 'expired', 'revoked'] },
}

const tokenFilters: FilterDef[] = [
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

// The signed-in account's access tokens (lbp_…) for the Lewbox API. A token
// acts as the account, so an instance admin's token can manage clusters and
// nodes.
export function AccessTokensSection({ instanceAdmin }: { instanceAdmin: boolean }) {
  const [createOpen, setCreateOpen] = useState(false)
  const [revokeTarget, setRevokeTarget] = useState<api.AccessToken | null>(null)
  const list = useListState(tokenSpec)
  const { state } = list
  const tokens = useLoad(`access-tokens:${list.key()}`, () =>
    api.listAccessTokens({
      ...listQuery(state, tokenSpec),
      status: state.filters.status as api.KeyStatus[] | undefined,
    }),
  )

  return (
    <div className="space-y-6">
      <FilterBar
        filters={tokenFilters}
        values={state.filters}
        onApply={list.setFilter}
        onRemove={list.removeFilter}
        onClear={list.clearFilters}
        leading={
          <SearchField
            label="Search access tokens"
            value={state.q}
            onSearch={list.setSearch}
          />
        }
      />
      {tokens.error.message && tokens.data && (
        <ErrorNotice error={tokens.error} onRetry={tokens.reload} />
      )}
      {tokens.error.message && !tokens.data ? (
        <ErrorNotice error={tokens.error} onRetry={tokens.reload} />
      ) : (
        <PaginatedTable
          page={tokens.data ?? undefined}
          loading={tokens.loading || tokens.stale}
          label="Access tokens"
          href={list.href}
          rowKey={(token) => token.id}
          head={
            <TableRow>
              <SortHeader state={state} spec={tokenSpec} column="name" base={list.params}>
                Name
              </SortHeader>
              <TableHeader>Token</TableHeader>
              <TableHeader>Status</TableHeader>
              <TableHeader>Last used</TableHeader>
              <SortHeader state={state} spec={tokenSpec} column="expires" base={list.params}>
                Expires
              </SortHeader>
              <TableHeader className="w-0">
                <span className="sr-only">Actions</span>
              </TableHeader>
            </TableRow>
          }
          row={(token) => (
            <TableRow key={token.id}>
              <TableCell
                className="font-medium"
                title={`Created ${formatDate(token.created_at)}`}
              >
                {token.name}
              </TableCell>
              <TableCell>
                <Code>
                  {token.prefix}…{token.last4}
                </Code>
              </TableCell>
              <TableCell>
                <TokenStatus status={token.status} />
              </TableCell>
              <TableCell
                className="text-zinc-500 dark:text-zinc-400"
                title={formatDate(token.last_used_at)}
              >
                {relativeTime(token.last_used_at)}
              </TableCell>
              <TableCell
                className="text-zinc-500 dark:text-zinc-400"
                title={token.expires_at ? formatDate(token.expires_at) : undefined}
              >
                {token.status === 'revoked'
                  ? `Revoked ${relativeTime(token.revoked_at).toLowerCase()}`
                  : token.expires_at
                    ? relativeTime(token.expires_at)
                    : 'Never'}
              </TableCell>
              <TableCell className="w-0 text-right">
                {token.status !== 'revoked' && (
                  <Dropdown>
                    <DropdownButton as={Button} plain aria-label={`Actions for ${token.name}`}>
                      <EllipsisHorizontalIcon />
                    </DropdownButton>
                    <DropdownMenu anchor="bottom end">
                      <DropdownItem
                        variant="danger"
                        onClick={() => setRevokeTarget(token)}
                      >
                        <NoSymbolIcon data-slot="icon" />
                        Revoke token
                      </DropdownItem>
                    </DropdownMenu>
                  </Dropdown>
                )}
              </TableCell>
            </TableRow>
          )}
          empty={
            isFiltered(state) ? (
              <NoMatches label="access tokens" onClear={list.clearAll} />
            ) : (
              <NoRecords
                title="No access tokens"
                description={
                  instanceAdmin
                    ? 'Lets scripts and CI use the CLI and API as you, including clusters and nodes.'
                    : 'Lets scripts and CI use the CLI and API as you.'
                }
              />
            )
          }
        />
      )}

      <div className="flex justify-end">
        <Button outline onClick={() => setCreateOpen(true)}>
          Create token
        </Button>
      </div>

      <CreateTokenDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={tokens.refresh}
      />
      <ConfirmationDialog
        open={revokeTarget !== null}
        onClose={() => setRevokeTarget(null)}
        title={revokeTarget ? `Revoke ${revokeTarget.name}?` : 'Revoke token?'}
        description="It stops working at once. Anything still using it gets 401 errors. This can't be undone."
        confirm="Revoke token"
        variant="danger"
        onConfirm={async () => {
          if (revokeTarget) await api.revokeAccessToken(revokeTarget.id)
        }}
        onConfirmed={() => {
          toast.success('Token revoked.')
          tokens.refresh()
        }}
      />
    </div>
  )
}

// Also opened over the server dialogs, to sign a machine's CLI in.
export function CreateTokenDialog({
  open,
  onClose,
  onCreated,
  defaultName = '',
}: {
  open: boolean
  onClose: () => void
  onCreated: () => void
  defaultName?: string
}) {
  const count = useOpenCount(open)
  // Once a token exists, the dialog only closes from its Done button, so the
  // token isn't lost to a stray click outside it.
  const [created, setCreated] = useState(false)
  return (
    <Dialog open={open} onClose={created ? () => {} : onClose} size="xl">
      <CreateToken
        key={count}
        defaultName={defaultName}
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

function CreateToken({
  onClose,
  onCreated,
  defaultName,
}: {
  onClose: () => void
  onCreated: () => void
  defaultName: string
}) {
  const [name, setName] = useState(defaultName)
  const [expiresIn, setExpiresIn] = useState<api.KeyLifetime>('90d')
  const [currentPassword, setCurrentPassword] = useState('')
  const [currentError, setCurrentError] = useState('')
  const [token, setToken] = useState<api.NewAccessToken | null>(null)
  const create = useAction(
    () => api.createAccessToken({ name, expires_in: expiresIn, current_password: currentPassword }),
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
      setToken(result.value)
      onCreated()
    }
  }

  if (token) {
    return (
      <>
        <DialogTitle>Copy your access token</DialogTitle>
        <DialogDescription>
          {token.name} acts as you
          {token.expires_at ? ` until ${formatDate(token.expires_at)}` : ' until you revoke it'}.
        </DialogDescription>
        <DialogBody>
          <SecretReveal secret={token.token} label="Copy access token">
            <Steps aria-label="Using the token">
              <Step title="Sign in the CLI with it">
                <CodeBlock code={`lewbox login ${serverURL()} --token`} />
              </Step>
              <Step title="Or set it for one shell or CI job">
                <CodeBlock code={`export LEWBOX_API_URL=${serverURL()}\nexport LEWBOX_TOKEN=<token>`} />
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
      <DialogTitle>Create access token</DialogTitle>
      <DialogDescription>Lets scripts and CI use the Lewbox CLI and API as you, with your rights.</DialogDescription>
      <DialogBody>
        <FieldGroup>
          <ErrorNotice error={create.error} />
          <Field disabled={create.busy}>
            <Label>Name</Label>
            <Input
              required
              autoFocus
              value={name}
              placeholder="Deploy pipeline"
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
          Create token
        </Button>
      </DialogActions>
    </form>
  )
}
