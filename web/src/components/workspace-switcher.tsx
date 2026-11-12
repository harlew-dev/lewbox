import {
  Avatar,
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
  Dropdown,
  DropdownButton,
  DropdownDivider,
  DropdownItem,
  DropdownLabel,
  DropdownMenu,
  ErrorNotice,
  Field,
  Input,
  Label,
  NavbarItem,
  NavbarLabel,
  SidebarItem,
  SidebarLabel,
  toast,
  useAction,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import {
  CheckIcon,
  ChevronDownIcon,
  Cog8ToothIcon,
  PlusIcon,
  WrenchScrewdriverIcon,
} from '@heroicons/react/16/solid'
import { useState, type FormEvent } from 'react'
import { useLocation, useNavigate } from 'react-router'
import * as api from '../lib/api'
import { useAuth } from '../lib/auth'
import { useInstance } from '../lib/instance'
import { orgInitials, useOrganizations } from '../lib/organizations'
import { failureMessage } from '../lib/use-load'

// The current workspace, its settings, every workspace the member belongs to
// (a check on the current one), and "New workspace…". In the sidebar's
// header and, on phones, at the right of the top bar.
export function WorkspaceSwitcher({ navbar = false, className }: { navbar?: boolean; className?: string }) {
  const { user } = useAuth()
  const { organizations, current, select } = useOrganizations()
  const { signup } = useInstance()
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const [creating, setCreating] = useState(false)
  if (!current) return null

  const Item = navbar ? NavbarItem : SidebarItem
  const Label = navbar ? NavbarLabel : SidebarLabel
  const onSettings = pathname === '/settings' || pathname.startsWith('/settings/')
  const admin = user?.instance_admin === true
  const canCreate = signup !== 'closed' || admin

  return (
    <>
      <Dropdown>
        <DropdownButton as={Item} className={className} selected={onSettings} aria-label="Workspace menu">
          <Avatar square initials={orgInitials(current.name)} color="zinc" data-slot="avatar" />
          <Label>{current.name}</Label>
          <ChevronDownIcon />
        </DropdownButton>
        <DropdownMenu className="min-w-80 lg:min-w-64" anchor={navbar ? 'bottom end' : 'bottom start'}>
          <DropdownItem
            href="/settings"
            aria-current={onSettings ? 'page' : undefined}
            selected={onSettings}
          >
            <Cog8ToothIcon data-slot="icon" />
            <DropdownLabel>Workspace settings</DropdownLabel>
          </DropdownItem>
          <DropdownDivider />
          {organizations.map((org) => (
            <DropdownItem
              key={org.id}
              onClick={() => {
                select(org.slug)
                navigate('/')
              }}
              aria-current={org.id === current.id ? 'page' : undefined}
            >
              <Avatar square initials={orgInitials(org.name)} color="zinc" data-slot="avatar" />
              <DropdownLabel className="truncate">{org.name}</DropdownLabel>
              {org.id === current.id && (
                <span className="col-start-5 row-start-1 flex justify-end">
                  <CheckIcon className="size-4" />
                </span>
              )}
            </DropdownItem>
          ))}
          {canCreate && (
            <>
              <DropdownDivider />
              <DropdownItem onClick={() => setCreating(true)}>
                <PlusIcon data-slot="icon" />
                <DropdownLabel>New workspace…</DropdownLabel>
              </DropdownItem>
            </>
          )}
          {admin && (
            <>
              <DropdownDivider />
              <DropdownItem href="/admin">
                <WrenchScrewdriverIcon data-slot="icon" />
                <DropdownLabel>Operator console</DropdownLabel>
              </DropdownItem>
            </>
          )}
        </DropdownMenu>
      </Dropdown>

      <CreateWorkspaceDialog open={creating} onClose={() => setCreating(false)} />
    </>
  )
}

function CreateWorkspaceDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const opened = useOpenCount(open)
  return <CreateWorkspaceForm key={opened} open={open} onClose={onClose} />
}

function CreateWorkspaceForm({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { select } = useOrganizations()
  const { reloadOrganizations } = useAuth()
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const create = useAction(() => api.createOrganization({ name: name.trim() }), {
    toError: (failure) => ({ message: failureMessage(failure) }),
  })

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await create.run()
    if (!result.ok) return
    await reloadOrganizations()
    select(result.value.slug)
    onClose()
    navigate('/')
    toast.success(`${result.value.name} created.`)
  }

  return (
    <Dialog open={open} onClose={() => !create.busy && onClose()}>
      <form onSubmit={submit}>
        <DialogTitle>New workspace</DialogTitle>
        <DialogDescription>A fresh space for another team or another big idea.</DialogDescription>
        <DialogBody>
          <ErrorNotice error={create.error} className="mb-6" />
          <Field disabled={create.busy}>
            <Label>Workspace name</Label>
            <Input
              autoFocus
              required
              autoComplete="off"
              placeholder="e.g. Acme"
              value={name}
              onChange={(event) => setName(event.currentTarget.value)}
            />
          </Field>
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
            Create workspace
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  )
}
