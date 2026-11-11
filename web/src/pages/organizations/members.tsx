import {
  Button,
  ConfirmationDialog,
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
  Dropdown,
  DropdownButton,
  DropdownDivider,
  DropdownItem,
  DropdownMenu,
  ErrorNotice,
  Field,
  FieldGroup,
  Input,
  Label,
  ListSkeleton,
  RadioGroup,
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
import {
  ArrowRightStartOnRectangleIcon,
  EllipsisHorizontalIcon,
  ShieldCheckIcon,
  TrashIcon,
} from '@heroicons/react/16/solid'
import { useState, type FormEvent } from 'react'
import * as api from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { formatDate, relativeTime } from '../../lib/format'
import type { Permissions } from '../../lib/organizations'
import { failureMessage, useLoad } from '../../lib/use-load'
import { RoleBadge, roleOptions } from './roles'

// An organization's members, with role changes, removal and leaving. The
// page around it owns the "Add member" button and opens the dialog.
export function MembersSection({
  org,
  can,
  addOpen,
  onAddClose,
  onLeft,
}: {
  org: api.Organization
  can: Permissions
  addOpen: boolean
  onAddClose: () => void
  onLeft: () => void
}) {
  const { user } = useAuth()
  const members = useLoad(() => api.listMembers(org.slug), [org.slug])
  const list = members.data?.data ?? []
  const [roleTarget, setRoleTarget] = useState<api.Member | null>(null)
  const [removeTarget, setRemoveTarget] = useState<api.Member | null>(null)

  const leaving = removeTarget !== null && removeTarget.user_id === user?.id

  return (
    <>
      {members.loading && !members.data ? (
        <ListSkeleton label="Loading members" rows={4} />
      ) : members.error.message ? (
        <ErrorNotice error={members.error} onRetry={members.reload} />
      ) : list.length === 0 ? (
        <Text>No members yet.</Text>
      ) : (
        <Table>
          <TableHead>
            <TableRow>
              <TableHeader>Member</TableHeader>
              <TableHeader>Role</TableHeader>
              <TableHeader>Added</TableHeader>
              <TableHeader className="w-0">
                <span className="sr-only">Actions</span>
              </TableHeader>
            </TableRow>
          </TableHead>
          <TableBody>
            {list.map((member) => {
              const self = member.user_id === user?.id
              const changeable = can.manage && (member.role !== 'owner' || can.owner)
              return (
                <TableRow key={member.id}>
                  <TableCell className="font-medium">
                    {user?.instance_admin ? (
                      <TextLink href={`/admin/users/${member.user_id}`}>{member.email}</TextLink>
                    ) : (
                      member.email
                    )}
                    {self && <span className="ml-2 text-zinc-500 dark:text-zinc-400">(you)</span>}
                  </TableCell>
                  <TableCell>
                    <RoleBadge role={member.role} />
                  </TableCell>
                  <TableCell className="text-zinc-500 dark:text-zinc-400" title={formatDate(member.created_at)}>
                    {relativeTime(member.created_at)}
                  </TableCell>
                  <TableCell className="w-0 text-right">
                    {(changeable || self) && (
                      <Dropdown>
                        <DropdownButton as={Button} plain aria-label={`Actions for ${member.email}`}>
                          <EllipsisHorizontalIcon />
                        </DropdownButton>
                        <DropdownMenu anchor="bottom end">
                          {changeable && (
                            <DropdownItem onClick={() => setRoleTarget(member)}>
                              <ShieldCheckIcon data-slot="icon" />
                              Change role
                            </DropdownItem>
                          )}
                          {changeable && !self && <DropdownDivider />}
                          {self ? (
                            <DropdownItem onClick={() => setRemoveTarget(member)}>
                              <ArrowRightStartOnRectangleIcon data-slot="icon" />
                              Leave organization
                            </DropdownItem>
                          ) : (
                            <DropdownItem variant="danger" onClick={() => setRemoveTarget(member)}>
                              <TrashIcon data-slot="icon" />
                              Remove member
                            </DropdownItem>
                          )}
                        </DropdownMenu>
                      </Dropdown>
                    )}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      )}

      <AddMemberDialog open={addOpen} org={org} canOwner={can.owner} onClose={onAddClose} onAdded={members.refresh} />
      <ChangeRoleDialog
        member={roleTarget}
        org={org}
        canOwner={can.owner}
        onClose={() => setRoleTarget(null)}
        onChanged={members.refresh}
      />
      <ConfirmationDialog
        open={removeTarget !== null}
        onClose={() => setRemoveTarget(null)}
        title={leaving ? `Leave ${org.name}?` : removeTarget ? `Remove ${removeTarget.email}?` : 'Remove member?'}
        description={
          leaving
            ? 'You lose access to this organization until someone adds you again.'
            : 'They lose access to this organization. Their account stays.'
        }
        confirm={leaving ? 'Leave organization' : 'Remove member'}
        variant="danger"
        onConfirm={async () => {
          if (removeTarget) await api.removeMember(org.slug, removeTarget.id)
        }}
        onConfirmed={() => {
          toast.success(leaving ? `You left ${org.name}.` : 'Member removed.')
          if (leaving) onLeft()
          else members.refresh()
        }}
      />
    </>
  )
}

function availableRoles(canOwner: boolean) {
  return roleOptions.filter((option) => canOwner || option.value !== 'owner')
}

function AddMemberDialog({
  open,
  org,
  canOwner,
  onClose,
  onAdded,
}: {
  open: boolean
  org: api.Organization
  canOwner: boolean
  onClose: () => void
  onAdded: () => void
}) {
  const count = useOpenCount(open)
  return (
    <Dialog open={open} onClose={onClose} size="md">
      <AddMember key={count} org={org} canOwner={canOwner} onClose={onClose} onAdded={onAdded} />
    </Dialog>
  )
}

function AddMember({
  org,
  canOwner,
  onClose,
  onAdded,
}: {
  org: api.Organization
  canOwner: boolean
  onClose: () => void
  onAdded: () => void
}) {
  const [email, setEmail] = useState('')
  const [role, setRole] = useState<api.Role>('member')
  const add = useAction(() => api.addMember(org.slug, { email, role }), {
    toError: (failure) => ({ message: failureMessage(failure) }),
  })

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await add.run()
    if (result.ok) {
      toast.success(`${result.value.email} added.`)
      onAdded()
      onClose()
    }
  }

  return (
    <form onSubmit={submit}>
      <DialogTitle>Add a member</DialogTitle>
      <DialogDescription>Give someone who already has an account a role in {org.name}.</DialogDescription>
      <DialogBody>
        <FieldGroup>
          <ErrorNotice error={add.error} />
          <Field disabled={add.busy}>
            <Label>Email</Label>
            <Input
              type="email"
              required
              autoFocus
              value={email}
              onChange={(event) => setEmail(event.currentTarget.value.trim())}
            />
          </Field>
          <RadioGroup
            label="Role"
            value={role}
            onChange={setRole}
            options={availableRoles(canOwner)}
            disabled={add.busy}
          />
        </FieldGroup>
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={add.busy}>
          Cancel
        </Button>
        <Button type="submit" loading={add.loading} disabled={email === ''} disabledReason="Fill in required fields">
          Add member
        </Button>
      </DialogActions>
    </form>
  )
}

function ChangeRoleDialog({
  member,
  org,
  canOwner,
  onClose,
  onChanged,
}: {
  member: api.Member | null
  org: api.Organization
  canOwner: boolean
  onClose: () => void
  onChanged: () => void
}) {
  const count = useOpenCount(member !== null)
  return (
    <Dialog open={member !== null} onClose={onClose} size="md">
      {member && (
        <ChangeRole key={count} member={member} org={org} canOwner={canOwner} onClose={onClose} onChanged={onChanged} />
      )}
    </Dialog>
  )
}

function ChangeRole({
  member,
  org,
  canOwner,
  onClose,
  onChanged,
}: {
  member: api.Member
  org: api.Organization
  canOwner: boolean
  onClose: () => void
  onChanged: () => void
}) {
  const [role, setRole] = useState<api.Role>(member.role)
  const save = useAction(() => api.setMemberRole(org.slug, member.id, role), {
    toError: (failure) => ({ message: failureMessage(failure) }),
  })

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await save.run()
    if (result.ok) {
      toast.success(`${member.email} is now ${role === 'admin' ? 'an' : 'a'} ${role}.`)
      onChanged()
      onClose()
    }
  }

  return (
    <form onSubmit={submit}>
      <DialogTitle>Change {member.email}'s role</DialogTitle>
      <DialogDescription>An organization always keeps at least one owner.</DialogDescription>
      <DialogBody>
        <div className="space-y-6">
          <ErrorNotice error={save.error} />
          <RadioGroup
            label="Role"
            value={role}
            onChange={setRole}
            options={availableRoles(canOwner)}
            disabled={save.busy}
          />
        </div>
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={save.busy}>
          Cancel
        </Button>
        <Button
          type="submit"
          loading={save.loading}
          disabled={role === member.role}
          disabledReason="No changes made"
        >
          Change role
        </Button>
      </DialogActions>
    </form>
  )
}
