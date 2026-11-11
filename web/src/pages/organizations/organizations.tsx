import {
  Avatar,
  Button,
  ConfirmationDialog,
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
  Description,
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
  PageHeader,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Link,
  Text,
  toast,
  useAction,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { ArrowPathIcon, EllipsisHorizontalIcon, PauseCircleIcon } from '@heroicons/react/16/solid'
import { useEffect, useState, type FormEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import * as api from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { formatDate, relativeTime } from '../../lib/format'
import { orgInitials, useOrganizations } from '../../lib/organizations'
import { useDocumentTitle } from '../../lib/use-document-title'
import { failureMessage } from '../../lib/use-load'
import { OrgStatusBadge, RoleBadge } from './roles'

export function OrganizationsPage() {
  useDocumentTitle('Organizations')
  const { user } = useAuth()
  const isAdmin = user?.instance_admin === true
  const { organizations, loading, reload } = useOrganizations()
  const [createOpen, setCreateOpen] = useState(false)
  const [searchParams, setSearchParams] = useSearchParams()
  // "New organization" in the sidebar's menu links here with ?new=1.
  useEffect(() => {
    if (searchParams.get('new') !== '1') return
    if (isAdmin) setCreateOpen(true)
    setSearchParams({}, { replace: true })
  }, [searchParams, setSearchParams, isAdmin])
  const [suspendTarget, setSuspendTarget] = useState<api.Organization | null>(null)

  const restore = useAction(
    async (org: api.Organization) => {
      await api.updateOrganization(org.slug, { status: 'active' })
      toast.success(`${org.name} restored.`)
      reload()
    },
    {
      onError: (failure) => {
        toast.error(failureMessage(failure))
        return true
      },
    },
  )

  return (
    <>
      <PageHeader
        title="Organizations"
        description={
          isAdmin
            ? 'Every organization on this instance. Each has its own members and API keys.'
            : 'The organizations you belong to.'
        }
        actions={isAdmin ? <Button onClick={() => setCreateOpen(true)}>New organization</Button> : undefined}
      />

      {loading && organizations.length === 0 ? (
        <ListSkeleton label="Loading organizations" rows={3} />
      ) : organizations.length === 0 ? (
        <EmptyState
          title="No organizations"
          description={
            isAdmin
              ? 'An organization groups the people and API keys of one business using Lewbox.'
              : 'Ask an instance admin or an organization owner to add you.'
          }
          action={isAdmin ? <Button onClick={() => setCreateOpen(true)}>New organization</Button> : undefined}
        />
      ) : (
        <Table>
          <TableHead>
            <TableRow>
              <TableHeader>Organization</TableHeader>
              <TableHeader>Status</TableHeader>
              <TableHeader className="text-right">Members</TableHeader>
              <TableHeader>Your role</TableHeader>
              <TableHeader>Created</TableHeader>
              {isAdmin && (
                <TableHeader className="w-0">
                  <span className="sr-only">Actions</span>
                </TableHeader>
              )}
            </TableRow>
          </TableHead>
          <TableBody>
            {organizations.map((org) => (
              <TableRow key={org.id}>
                <TableCell>
                  <div className="flex items-center gap-3">
                    <Avatar initials={orgInitials(org.name)} color="zinc" className="size-8" />
                    <div>
                      <Link
                        href={`/organizations/${org.slug}/members`}
                        className="font-medium text-zinc-950 data-hover:underline dark:text-white"
                      >
                        {org.name}
                      </Link>
                      <Text size="sm">{org.slug}</Text>
                    </div>
                  </div>
                </TableCell>
                <TableCell>
                  <OrgStatusBadge status={org.status} />
                </TableCell>
                <TableCell className="text-right tabular-nums">{org.members}</TableCell>
                <TableCell>{org.role ? <RoleBadge role={org.role} /> : <Text>—</Text>}</TableCell>
                <TableCell className="text-zinc-500 dark:text-zinc-400" title={formatDate(org.created_at)}>
                  {relativeTime(org.created_at)}
                </TableCell>
                {isAdmin && (
                  <TableCell className="w-0 text-right">
                    <Dropdown>
                      <DropdownButton as={Button} plain aria-label={`Actions for ${org.name}`}>
                        <EllipsisHorizontalIcon />
                      </DropdownButton>
                      <DropdownMenu anchor="bottom end">
                        <DropdownItem href={`/organizations/${org.slug}/settings`}>Settings</DropdownItem>
                        {org.status === 'active' ? (
                          <DropdownItem variant="danger" onClick={() => setSuspendTarget(org)}>
                            <PauseCircleIcon data-slot="icon" />
                            Suspend organization
                          </DropdownItem>
                        ) : (
                          <DropdownItem disabled={restore.busy} onClick={() => void restore.run(org)}>
                            <ArrowPathIcon data-slot="icon" />
                            Restore organization
                          </DropdownItem>
                        )}
                      </DropdownMenu>
                    </Dropdown>
                  </TableCell>
                )}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <CreateOrganizationDialog open={createOpen} onClose={() => setCreateOpen(false)} onCreated={reload} />
      <SuspendOrganizationDialog
        org={suspendTarget}
        onClose={() => setSuspendTarget(null)}
        onSuspended={reload}
      />
    </>
  )
}

export function SuspendOrganizationDialog({
  org,
  onClose,
  onSuspended,
}: {
  org: api.Organization | null
  onClose: () => void
  onSuspended: () => void
}) {
  return (
    <ConfirmationDialog
      open={org !== null}
      onClose={onClose}
      title={org ? `Suspend ${org.name}?` : 'Suspend organization?'}
      description="Its API keys stop working at once, and its members can only look until you restore it. Nothing is deleted."
      confirm="Suspend organization"
      variant="danger"
      onConfirm={async () => {
        if (org) await api.updateOrganization(org.slug, { status: 'suspended' })
      }}
      onConfirmed={() => {
        toast.success('Organization suspended.')
        onSuspended()
      }}
    />
  )
}

function CreateOrganizationDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean
  onClose: () => void
  onCreated: () => void
}) {
  const count = useOpenCount(open)
  return (
    <Dialog open={open} onClose={onClose} size="md">
      <CreateOrganization key={count} onClose={onClose} onCreated={onCreated} />
    </Dialog>
  )
}

function CreateOrganization({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const navigate = useNavigate()
  const { user } = useAuth()
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [slugEdited, setSlugEdited] = useState(false)
  const [owner, setOwner] = useState('')

  const create = useAction(() => api.createOrganization({ slug, name, owner }), {
    toError: (failure) => ({ message: failureMessage(failure) }),
  })

  function changeName(value: string) {
    setName(value)
    if (!slugEdited) {
      setSlug(
        value
          .toLowerCase()
          .replace(/[^a-z0-9]+/g, '-')
          .replace(/^-+|-+$/g, '')
          .slice(0, 63),
      )
    }
  }

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await create.run()
    if (result.ok) {
      toast.success(`${result.value.name} created.`)
      onCreated()
      onClose()
      navigate(`/organizations/${result.value.slug}/members`)
    }
  }

  return (
    <form onSubmit={submit}>
      <DialogTitle>New organization</DialogTitle>
      <DialogDescription>A business using Lewbox, with its own members and API keys.</DialogDescription>
      <DialogBody>
        <FieldGroup>
          <ErrorNotice error={create.error} />
          <Field disabled={create.busy}>
            <Label>Name</Label>
            <Input required value={name} placeholder="Acme" onChange={(event) => changeName(event.currentTarget.value)} />
          </Field>
          <Field disabled={create.busy}>
            <Label>Slug</Label>
            <Input
              required
              value={slug}
              placeholder="acme"
              onChange={(event) => {
                setSlugEdited(true)
                setSlug(event.currentTarget.value.trim())
              }}
            />
            <Description>Used in URLs and with the CLI's --org. It can't change later.</Description>
          </Field>
          <Field disabled={create.busy}>
            <Label>Owner</Label>
            <Input
              type="email"
              value={owner}
              placeholder={user?.email ?? 'you'}
              onChange={(event) => setOwner(event.currentTarget.value.trim())}
            />
            <Description>An existing account's email. Leave it empty to own it yourself.</Description>
          </Field>
        </FieldGroup>
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={create.busy}>
          Cancel
        </Button>
        <Button type="submit" loading={create.loading} disabled={slug === ''} disabledReason="Fill in required fields">
          Create organization
        </Button>
      </DialogActions>
    </form>
  )
}
