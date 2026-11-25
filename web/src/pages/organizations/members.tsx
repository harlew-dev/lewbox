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
  PaginatedTable,
  RadioGroup,
  SearchField,
  TableCell,
  TableHeader,
  TableRow,
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
import { FilterBar, type FilterDef } from '../../components/filter-bar'
import { NoMatches, NoRecords, SortHeader } from '../../components/list'
import { formatDate, relativeTime } from '../../lib/format'
import { isFiltered, listQuery, type ListSpec } from '../../lib/list-query'
import type { Permissions } from '../../lib/organizations'
import { routes } from '../../lib/routes'
import { useListState } from '../../lib/use-list'
import { failureMessage, useLoad } from '../../lib/use-load'
import { RoleBadge, roleOptions } from './roles'

// An organization's members, with role changes, removal and leaving. The
// page around it owns the "Add member" button and opens the dialog.
const memberSpec: ListSpec = {
  sortKeys: ['email', 'role', 'joined'],
  defaultSort: 'email',
  descendingFirst: ['joined'],
  filters: { role: ['owner', 'admin', 'member', 'viewer'] },
}

const memberFilters: FilterDef[] = [
  {
    key: 'role',
    label: 'Role',
    options: roleOptions.map((option) => ({ value: option.value, label: option.label })),
  },
]

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
  const list = useListState(memberSpec)
  const { state } = list
  const members = useLoad(`members:${org.slug}:${list.key()}`, () =>
    api.listMembers(org.slug, {
      ...listQuery(state, memberSpec),
      role: state.filters.role as api.Role[] | undefined,
    }),
  )
  const [roleTarget, setRoleTarget] = useState<api.Member | null>(null)
  const [removeTarget, setRemoveTarget] = useState<api.Member | null>(null)

  const leaving = removeTarget !== null && removeTarget.user_id === user?.id

  return (
    <>
      <div className="space-y-4">
        <FilterBar
          filters={memberFilters}
          values={state.filters}
          onApply={list.setFilter}
          onRemove={list.removeFilter}
          onClear={list.clearFilters}
          leading={
            <SearchField
              label="Search members"
              value={state.q}
              onSearch={list.setSearch}
            />
          }
        />
        {members.error.message && members.data && (
          <ErrorNotice error={members.error} onRetry={members.reload} />
        )}
        {members.error.message && !members.data ? (
          <ErrorNotice error={members.error} onRetry={members.reload} />
        ) : (
          <PaginatedTable
            page={members.data ?? undefined}
            loading={members.loading || members.stale}
            label="Members"
            href={list.href}
            rowKey={(member) => member.id}
            head={
              <TableRow>
                <SortHeader state={state} spec={memberSpec} column="email" base={list.params}>
                  Member
                </SortHeader>
                <SortHeader state={state} spec={memberSpec} column="role" base={list.params}>
                  Role
                </SortHeader>
                <SortHeader state={state} spec={memberSpec} column="joined" base={list.params}>
                  Added
                </SortHeader>
                <TableHeader className="w-0">
                  <span className="sr-only">Actions</span>
                </TableHeader>
              </TableRow>
            }
            row={(member) => {
              const self = member.user_id === user?.id
              const changeable = can.manage && (member.role !== 'owner' || can.owner)
              return (
                <TableRow key={member.id}>
                  <TableCell className="font-medium">
                    {user?.instance_admin ? (
                      <TextLink href={routes.admin.user(member.user_id)}>
                        {member.email}
                      </TextLink>
                    ) : (
                      member.email
                    )}
                    {self && (
                      <span className="ml-2 text-zinc-500 dark:text-zinc-400">(you)</span>
                    )}
                  </TableCell>
                  <TableCell>
                    <RoleBadge role={member.role} />
                  </TableCell>
                  <TableCell
                    className="text-zinc-500 dark:text-zinc-400"
                    title={formatDate(member.created_at)}
                  >
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
                              Leave workspace
                            </DropdownItem>
                          ) : (
                            <DropdownItem
                              variant="danger"
                              onClick={() => setRemoveTarget(member)}
                            >
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
            }}
            empty={
              isFiltered(state) ? (
                <NoMatches label="members" onClear={list.clearAll} />
              ) : (
                <NoRecords title="No members yet" description="Add someone to this workspace." />
              )
            }
          />
        )}
      </div>

      <AddMemberDialog
        open={addOpen}
        org={org}
        canOwner={can.owner}
        onClose={onAddClose}
        onAdded={members.refresh}
      />
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
        title={
          leaving
            ? `Leave ${org.name}?`
            : removeTarget
              ? `Remove ${removeTarget.email}?`
              : 'Remove member?'
        }
        description={
          leaving
            ? 'You lose access to this workspace until someone adds you again.'
            : 'They lose access to this workspace. Their account stays.'
        }
        confirm={leaving ? 'Leave workspace' : 'Remove member'}
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
      <DialogDescription>A workspace always keeps at least one owner.</DialogDescription>
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
