import {
  Badge,
  Button,
  CheckboxField,
  Description,
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
  ErrorNotice,
  Field,
  FieldGroup,
  Input,
  Label,
  PageHeader,
  PaginatedTable,
  SearchField,
  TableCell,
  TableHeader,
  TableRow,
  TextLink,
  toast,
  useAction,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router'
import * as api from '../../lib/api'
import { FilterBar, type FilterDef } from '../../components/filter-bar'
import { NoMatches, NoRecords, SortHeader } from '../../components/list'
import { relativeTime } from '../../lib/format'
import { isFiltered, listQuery, type ListSpec } from '../../lib/list-query'
import { routes } from '../../lib/routes'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useListState } from '../../lib/use-list'
import { failureMessage, useLoad } from '../../lib/use-load'
import { UserActions } from './user-actions'

export function UserBadges({ user }: { user: api.User }) {
  return (
    <>
      {user.instance_admin && <Badge color="purple">Instance admin</Badge>}
      {user.disabled ? <Badge color="red">Disabled</Badge> : <Badge color="lime">Active</Badge>}
    </>
  )
}

const spec: ListSpec = {
  sortKeys: ['email', 'created', 'status'],
  defaultSort: 'email',
  descendingFirst: ['created'],
  filters: {
    status: ['active', 'disabled'],
    role: ['admin', 'user'],
  },
}

const filters: FilterDef[] = [
  {
    key: 'status',
    label: 'Status',
    options: [
      { value: 'active', label: 'Active' },
      { value: 'disabled', label: 'Disabled' },
    ],
  },
  {
    key: 'role',
    label: 'Role',
    options: [
      { value: 'admin', label: 'Instance admin' },
      { value: 'user', label: 'User' },
    ],
  },
]

export function UsersPage() {
  useDocumentTitle('Users')
  const list = useListState(spec)
  const { state } = list
  const users = useLoad(`users:${list.key()}`, () =>
    api.listUsers({
      ...listQuery(state, spec),
      status: state.filters.status as api.UserStatus[] | undefined,
      role: state.filters.role as api.UserRole[] | undefined,
    }),
  )
  const [createOpen, setCreateOpen] = useState(false)
  const filtered = isFiltered(state)

  return (
    <>
      <PageHeader
        title="Users"
        description="Every account that can sign in to this Lewbox instance."
        actions={<Button onClick={() => setCreateOpen(true)}>New account</Button>}
      />

      <div className="space-y-4">
        <FilterBar
          filters={filters}
          values={state.filters}
          onApply={list.setFilter}
          onRemove={list.removeFilter}
          onClear={list.clearFilters}
          leading={
            <SearchField label="Search users" value={state.q} onSearch={list.setSearch} />
          }
        />
        {users.error.message && users.data && (
          <ErrorNotice error={users.error} onRetry={users.reload} />
        )}
        {users.error.message && !users.data ? (
          <ErrorNotice error={users.error} onRetry={users.reload} />
        ) : (
          <PaginatedTable
            page={users.data ?? undefined}
            loading={users.loading || users.stale}
            label="Users"
            href={list.href}
            rowKey={(user) => user.id}
            head={
              <TableRow>
                <SortHeader state={state} spec={spec} column="email" base={list.params}>
                  Email
                </SortHeader>
                <SortHeader state={state} spec={spec} column="status" base={list.params}>
                  Status
                </SortHeader>
                <SortHeader state={state} spec={spec} column="created" base={list.params}>
                  Created
                </SortHeader>
                <TableHeader className="w-0">
                  <span className="sr-only">Actions</span>
                </TableHeader>
              </TableRow>
            }
            row={(user) => (
              <TableRow key={user.id}>
                <TableCell className="font-medium">
                  <TextLink href={routes.admin.user(user.id)}>{user.email}</TextLink>
                </TableCell>
                <TableCell>
                  <div className="flex flex-wrap gap-2">
                    <UserBadges user={user} />
                  </div>
                </TableCell>
                <TableCell
                  className="text-zinc-500 dark:text-zinc-400"
                  title={new Date(user.created_at).toLocaleString()}
                >
                  {relativeTime(user.created_at)}
                </TableCell>
                <TableCell className="w-0 text-right">
                  <UserActions user={user} onChanged={users.reload} onRemoved={users.reload} />
                </TableCell>
              </TableRow>
            )}
            empty={
              filtered ? (
                <NoMatches label="users" onClear={list.clearAll} />
              ) : (
                <NoRecords
                  title="No accounts yet"
                  description="Create one to let someone sign in to this instance."
                />
              )
            }
          />
        )}
      </div>

      <CreateUserDialog open={createOpen} onClose={() => setCreateOpen(false)} />
    </>
  )
}

function CreateUserDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const count = useOpenCount(open)
  return (
    <Dialog open={open} onClose={onClose} size="md">
      <CreateUser key={count} onClose={onClose} />
    </Dialog>
  )
}

function CreateUser({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [isAdmin, setIsAdmin] = useState(false)
  const create = useAction(() => api.createUser({ email, password, instance_admin: isAdmin }), {
    toError: (failure) => ({ message: failureMessage(failure) }),
  })

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await create.run()
    if (result.ok) {
      toast.success('Account created.')
      onClose()
      navigate(routes.admin.user(result.value.id))
    }
  }

  return (
    <form onSubmit={submit}>
      <DialogTitle>New account</DialogTitle>
      <DialogDescription>
        The account can sign in straight away. Add it to a workspace from its page.
      </DialogDescription>
      <ErrorNotice error={create.error} className="mt-6" />
      <DialogBody className="space-y-6">
        <FieldGroup>
          <Field disabled={create.busy}>
            <Label>Email</Label>
            <Input
              type="email"
              autoComplete="off"
              required
              value={email}
              onChange={(event) => setEmail(event.currentTarget.value)}
            />
          </Field>
          <Field disabled={create.busy}>
            <Label>Password</Label>
            <Input
              type="password"
              autoComplete="new-password"
              required
              value={password}
              onChange={(event) => setPassword(event.currentTarget.value)}
            />
            <Description>At least 12 characters.</Description>
          </Field>
        </FieldGroup>
        <CheckboxField checked={isAdmin} onChange={setIsAdmin} disabled={create.busy}>
          Instance admin: manages every server, workspace and account
        </CheckboxField>
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={create.busy}>
          Cancel
        </Button>
        <Button type="submit" loading={create.loading}>
          Create account
        </Button>
      </DialogActions>
    </form>
  )
}
