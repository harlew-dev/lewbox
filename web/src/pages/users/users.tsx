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
  ListSkeleton,
  PageHeader,
  Table,
  TableBody,
  TableCell,
  TableHead,
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
import { relativeTime } from '../../lib/format'
import { useDocumentTitle } from '../../lib/use-document-title'
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

export function UsersPage() {
  useDocumentTitle('Users')
  const users = useLoad(() => api.listUsers(), [])
  const userList = users.data?.data ?? []
  const [createOpen, setCreateOpen] = useState(false)

  return (
    <>
      <PageHeader
        title="Users"
        description="Every account that can sign in to this Lewbox instance."
        actions={<Button onClick={() => setCreateOpen(true)}>New account</Button>}
      />

      {users.loading && !users.data ? (
        <ListSkeleton label="Loading users" rows={4} />
      ) : users.error.message ? (
        <ErrorNotice error={users.error} onRetry={users.reload} />
      ) : (
        <Table>
          <TableHead>
            <TableRow>
              <TableHeader>Email</TableHeader>
              <TableHeader>Status</TableHeader>
              <TableHeader>Created</TableHeader>
              <TableHeader className="w-0">
                <span className="sr-only">Actions</span>
              </TableHeader>
            </TableRow>
          </TableHead>
          <TableBody>
            {userList.map((user) => (
              <TableRow key={user.id}>
                <TableCell className="font-medium">
                  <TextLink href={`/admin/users/${user.id}`}>{user.email}</TextLink>
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
            ))}
          </TableBody>
        </Table>
      )}

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
      navigate(`/admin/users/${result.value.id}`)
    }
  }

  return (
    <form onSubmit={submit}>
      <DialogTitle>New account</DialogTitle>
      <DialogDescription>
        The account can sign in straight away. Add it to an organization from its page.
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
          Instance admin: manages every server, organization and account
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
