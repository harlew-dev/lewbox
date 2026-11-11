import {
  Avatar,
  Button,
  ConfirmationDialog,
  Description,
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
  EmptyState,
  ErrorNotice,
  Field,
  FieldGroup,
  Input,
  Label,
  ListSkeleton,
  PageHeader,
  PageTabs,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Text,
  tabFrom,
  toast,
  useAction,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { useState, type FormEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { useRefreshPending } from '../../components/console-layout'
import * as api from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { formatMoney, formatNumber } from '../../lib/format'
import { useInstance } from '../../lib/instance'
import { orgInitials } from '../../lib/organizations'
import { useDocumentTitle } from '../../lib/use-document-title'
import { failureMessage, useLoad } from '../../lib/use-load'
import { OrgStatusBadge } from '../organizations/roles'

const tabs = [
  { id: 'all', label: 'All' },
  { id: 'pending', label: 'Pending' },
  { id: 'suspended', label: 'Suspended' },
] as const

const empty = {
  all: { title: 'No customers yet', description: 'Customers appear here when they sign up, or when you add one.' },
  pending: { title: 'No one is waiting', description: 'Sign-ups wait here for approval when sign-up needs it.' },
  suspended: { title: 'No suspended customers', description: 'Customers you suspend, or who run out of credits, show here.' },
}

export function CustomersPage() {
  useDocumentTitle('Customers')
  const [searchParams] = useSearchParams()
  const tab = tabFrom(searchParams, tabs)
  const { billing, currency } = useInstance()
  const [createOpen, setCreateOpen] = useState(false)
  const customers = useLoad(() => api.listOrganizations({ status: tab === 'all' ? undefined : tab }), [tab])
  const running = useLoad(() => api.listSandboxes({ status: 'started' }), [])
  const list = customers.data?.data ?? []

  const runningBy = new Map<string, number>()
  for (const sandbox of running.data?.data ?? []) {
    const slug = sandbox.organization?.slug
    if (slug) runningBy.set(slug, (runningBy.get(slug) ?? 0) + 1)
  }

  return (
    <>
      <PageHeader title="Customers" actions={<Button onClick={() => setCreateOpen(true)}>New customer</Button>} />
      <PageTabs label="Customer views" tabs={tabs} current={tab} className="mb-8" />

      {customers.loading && !customers.data ? (
        <ListSkeleton label="Loading customers" rows={4} />
      ) : customers.error.message ? (
        <ErrorNotice error={customers.error} onRetry={customers.reload} />
      ) : list.length === 0 ? (
        <EmptyState {...empty[tab]} />
      ) : (
        <Table>
          <TableHead>
            <TableRow>
              <TableHeader>Customer</TableHeader>
              <TableHeader>Plan</TableHeader>
              <TableHeader>Status</TableHeader>
              <TableHeader className="text-right">Running</TableHeader>
              <TableHeader className="text-right">vCPU-hours (30 d)</TableHeader>
              {billing !== 'off' && <TableHeader className="text-right">Balance</TableHeader>}
            </TableRow>
          </TableHead>
          <TableBody>
            {list.map((org) => (
              <TableRow key={org.id} href={`/admin/customers/${encodeURIComponent(org.slug)}`} title={`Open ${org.name}`}>
                <TableCell>
                  <div className="flex items-center gap-3">
                    <Avatar initials={orgInitials(org.name)} color="zinc" className="size-8" />
                    <div>
                      <div className="font-medium">{org.name}</div>
                      <Text size="sm">{org.slug}</Text>
                    </div>
                  </div>
                </TableCell>
                <TableCell>{org.plan?.name ?? '—'}</TableCell>
                <TableCell>
                  <OrgStatusBadge status={org.status} reason={org.status_reason} />
                </TableCell>
                <TableCell className="text-right tabular-nums">{runningBy.get(org.slug) ?? 0}</TableCell>
                <TableCell className="text-right tabular-nums">
                  {org.vcpu_hours_30d === undefined ? '—' : formatNumber(org.vcpu_hours_30d, 1)}
                </TableCell>
                {billing !== 'off' && (
                  <TableCell className="text-right tabular-nums">
                    {org.balance_micros === undefined ? '—' : formatMoney(org.balance_micros, currency)}
                  </TableCell>
                )}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <CreateCustomerDialog open={createOpen} onClose={() => setCreateOpen(false)} />
    </>
  )
}

export function SuspendDialog({
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
      title={org ? `Suspend ${org.name}?` : 'Suspend customer?'}
      description="Its sandboxes are stopped and its API keys stop working until you restore it. Nothing is deleted."
      confirm="Suspend customer"
      variant="danger"
      onConfirm={async () => {
        if (org) await api.updateOrganization(org.slug, { status: 'suspended' })
      }}
      onConfirmed={() => {
        toast.success('Customer suspended.')
        onSuspended()
      }}
    />
  )
}

function CreateCustomerDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const count = useOpenCount(open)
  return (
    <Dialog open={open} onClose={onClose} size="md">
      <CreateCustomer key={count} onClose={onClose} />
    </Dialog>
  )
}

function CreateCustomer({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate()
  const refreshPending = useRefreshPending()
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
      refreshPending()
      onClose()
      navigate(`/admin/customers/${encodeURIComponent(result.value.slug)}`)
    }
  }

  return (
    <form onSubmit={submit}>
      <DialogTitle>New customer</DialogTitle>
      <DialogDescription>An organization on the default plan, owned by an existing account.</DialogDescription>
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
            <Description>Used in URLs and with the CLI’s --org. It can’t change later.</Description>
          </Field>
          <Field disabled={create.busy}>
            <Label>Owner</Label>
            <Input
              type="email"
              value={owner}
              placeholder={user?.email ?? 'you'}
              onChange={(event) => setOwner(event.currentTarget.value.trim())}
            />
            <Description>An existing account’s email. Leave it empty to own it yourself.</Description>
          </Field>
        </FieldGroup>
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={create.busy}>
          Cancel
        </Button>
        <Button type="submit" loading={create.loading} disabled={slug === ''} disabledReason="Fill in required fields">
          Create customer
        </Button>
      </DialogActions>
    </form>
  )
}
