import {
  Button,
  DescriptionDetails,
  DescriptionList,
  DescriptionTerm,
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
  Divider,
  EmptyState,
  ErrorNotice,
  Field,
  FieldGroup,
  Label,
  Listbox,
  ListboxOption,
  ListSkeleton,
  PageHeader,
  RadioGroup,
  Subheading,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Text,
  TextLink,
  toast,
  useAction,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router'
import * as api from '../../lib/api'
import { formatDate, relativeTime } from '../../lib/format'
import { useAuth } from '../../lib/auth'
import { useDocumentTitle } from '../../lib/use-document-title'
import { failureMessage, useLoad } from '../../lib/use-load'
import { RoleBadge, roleOptions } from '../organizations/roles'
import { UserActions } from './user-actions'
import { UserBadges } from './users'

const back = { href: '/admin/users', label: 'Users' }

// One account: what it is, which organizations it belongs to, and what an
// instance admin can do to it.
export function UserDetailPage() {
  const { ref = '' } = useParams()
  const navigate = useNavigate()
  const { reloadOrganizations } = useAuth()
  const all = useLoad(() => api.listOrganizations(), [])
  const user = useLoad(() => api.getUser(ref), [ref])
  const orgs = useLoad(() => api.listOrganizations({ user: ref }), [ref])
  const [addOpen, setAddOpen] = useState(false)
  useDocumentTitle(user.data?.email ?? 'User')

  if (user.loading && !user.data) {
    return (
      <>
        <PageHeader title="User" back={back} />
        <ListSkeleton label="Loading the account" rows={4} />
      </>
    )
  }
  if (user.failure instanceof api.ApiError && user.failure.status === 404) {
    return (
      <EmptyState
        title="Account not found"
        description="It may have been removed."
        action={<Button href="/admin/users">Back to users</Button>}
      />
    )
  }
  if (user.error.message || !user.data) {
    return (
      <>
        <PageHeader title="User" back={back} />
        <ErrorNotice error={user.error} onRetry={user.reload} />
      </>
    )
  }

  const account = user.data
  const memberships = orgs.data?.data ?? []
  const joinable = (all.data?.data ?? []).filter(
    (org) => !memberships.some((member) => member.id === org.id),
  )

  return (
    <>
      <PageHeader
        back={back}
        title={account.email}
        meta={<UserBadges user={account} />}
        actions={
          <UserActions user={account} onChanged={user.reload} onRemoved={() => navigate('/admin/users')} />
        }
      />

      <Subheading className="mt-12">Account</Subheading>
      <Divider className="mt-4" />
      <DescriptionList>
        <DescriptionTerm>Email</DescriptionTerm>
        <DescriptionDetails>{account.email}</DescriptionDetails>
        <DescriptionTerm>ID</DescriptionTerm>
        <DescriptionDetails className="font-mono text-sm">{account.id}</DescriptionDetails>
        <DescriptionTerm>Instance admin</DescriptionTerm>
        <DescriptionDetails>{account.instance_admin ? 'Yes' : 'No'}</DescriptionDetails>
        <DescriptionTerm>Status</DescriptionTerm>
        <DescriptionDetails>{account.disabled ? 'Disabled: cannot sign in' : 'Active'}</DescriptionDetails>
        <DescriptionTerm>Created</DescriptionTerm>
        <DescriptionDetails title={formatDate(account.created_at)}>
          {relativeTime(account.created_at)}
        </DescriptionDetails>
      </DescriptionList>

      <div className="mt-12 flex flex-wrap items-end justify-between gap-4">
        <Subheading>Workspaces</Subheading>
        <Button outline disabled={joinable.length === 0} disabledReason="Already in every workspace" onClick={() => setAddOpen(true)}>
          Add to workspace
        </Button>
      </div>
      <div className="mt-4">
        {orgs.loading && !orgs.data ? (
          <ListSkeleton label="Loading workspaces" rows={2} />
        ) : orgs.error.message ? (
          <ErrorNotice error={orgs.error} onRetry={orgs.reload} />
        ) : memberships.length === 0 ? (
          <Text>Not a member of any workspace.</Text>
        ) : (
          <Table>
            <TableHead>
              <TableRow>
                <TableHeader>Workspace</TableHeader>
                <TableHeader>Role</TableHeader>
              </TableRow>
            </TableHead>
            <TableBody>
              {memberships.map((org) => (
                <TableRow key={org.id}>
                  <TableCell className="font-medium">
                    <TextLink href={`/admin/customers/${encodeURIComponent(org.slug)}`}>{org.name}</TextLink>
                  </TableCell>
                  <TableCell>{org.role && <RoleBadge role={org.role} />}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </div>

      <AddToOrganizationDialog
        open={addOpen}
        user={account}
        organizations={joinable}
        onClose={() => setAddOpen(false)}
        onAdded={() => {
          orgs.reload()
          void reloadOrganizations()
        }}
      />
    </>
  )
}

function AddToOrganizationDialog({
  open,
  user,
  organizations,
  onClose,
  onAdded,
}: {
  open: boolean
  user: api.User
  organizations: api.Organization[]
  onClose: () => void
  onAdded: () => void
}) {
  const count = useOpenCount(open)
  return (
    <Dialog open={open} onClose={onClose} size="md">
      <AddToOrganization key={count} user={user} organizations={organizations} onClose={onClose} onAdded={onAdded} />
    </Dialog>
  )
}

function AddToOrganization({
  user,
  organizations,
  onClose,
  onAdded,
}: {
  user: api.User
  organizations: api.Organization[]
  onClose: () => void
  onAdded: () => void
}) {
  const [org, setOrg] = useState(organizations[0]?.slug ?? '')
  const [role, setRole] = useState<api.Role>('member')
  const add = useAction(() => api.addMember(org, { email: user.email, role }), {
    toError: (failure) => ({ message: failureMessage(failure) }),
  })

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await add.run()
    if (result.ok) {
      toast.success(`${user.email} added.`)
      onAdded()
      onClose()
    }
  }

  return (
    <form onSubmit={submit}>
      <DialogTitle>Add to a workspace</DialogTitle>
      <DialogDescription>Give {user.email} a role in another workspace.</DialogDescription>
      <DialogBody>
        <FieldGroup>
          <ErrorNotice error={add.error} />
          <Field disabled={add.busy}>
            <Label>Workspace</Label>
            <Listbox value={org} onChange={setOrg}>
              {organizations.map((item) => (
                <ListboxOption key={item.id} value={item.slug}>
                  {item.name}
                </ListboxOption>
              ))}
            </Listbox>
          </Field>
          <RadioGroup label="Role" value={role} onChange={setRole} options={roleOptions} disabled={add.busy} />
        </FieldGroup>
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={add.busy}>
          Cancel
        </Button>
        <Button type="submit" loading={add.loading} disabled={org === ''} disabledReason="Fill in required fields">
          Add to workspace
        </Button>
      </DialogActions>
    </form>
  )
}
