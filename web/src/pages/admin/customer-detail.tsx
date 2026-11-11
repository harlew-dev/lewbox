import {
  Button,
  DescriptionDetails,
  DescriptionList,
  DescriptionTerm,
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
  Listbox,
  ListboxOption,
  ListSkeleton,
  PageHeader,
  PageTabs,
  Stat,
  Subheading,
  tabFrom,
  toast,
  useAction,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { useState, type FormEvent } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router'
import { useRefreshPending } from '../../components/console-layout'
import { LimitMeters } from '../../components/limit-meters'
import { SandboxTable } from '../../components/sandbox-table'
import { UsageSummary } from '../../components/usage-summary'
import * as api from '../../lib/api'
import { daysAgo, formatLimit, formatMemory, formatMoney, formatNumber } from '../../lib/format'
import { useInstance } from '../../lib/instance'
import { orgPermissions } from '../../lib/organizations'
import { useDocumentTitle } from '../../lib/use-document-title'
import { failureMessage, useLoad } from '../../lib/use-load'
import { MembersSection } from '../organizations/members'
import { OrgStatusBadge } from '../organizations/roles'
import { CreditTable } from '../portal/billing'
import { SandboxKeyTable } from '../portal/keys'
import { SuspendDialog } from './customers'

const back = { href: '/admin/customers', label: 'Customers' }

export function CustomerDetailPage() {
  const { org: slug = '' } = useParams()
  const navigate = useNavigate()
  const refreshPending = useRefreshPending()
  const { billing } = useInstance()
  const [searchParams] = useSearchParams()
  const customer = useLoad(() => api.getOrganization(slug), [slug])
  const org = customer.data
  useDocumentTitle(org?.name ?? 'Customer')
  const [dialog, setDialog] = useState<'limits' | 'credits' | 'suspend' | 'member' | null>(null)

  const tabs = [
    { id: 'overview', label: 'Overview' },
    { id: 'sandboxes', label: 'Sandboxes' },
    { id: 'keys', label: 'API keys' },
    { id: 'members', label: 'Members' },
    ...(billing !== 'off' ? [{ id: 'credits', label: 'Credits' }] : []),
  ]
  const tab = tabFrom(searchParams, tabs)

  const setStatus = useAction(
    async (status: api.OrgStatus) => {
      const updated = await api.updateOrganization(slug, { status })
      toast.success(status === 'active' && org?.status === 'pending' ? `${updated.name} approved.` : `${updated.name} restored.`)
      changed()
    },
    {
      onError: (failure) => {
        toast.error(failureMessage(failure))
        return true
      },
    },
  )

  function changed() {
    customer.refresh()
    refreshPending()
  }

  if (customer.loading && !org) return <ListSkeleton label="Loading the customer" rows={4} />
  if (customer.failure instanceof api.ApiError && customer.failure.status === 404) {
    return (
      <EmptyState
        title="Customer not found"
        description={`There is no customer ${slug}.`}
        action={<Button href="/admin/customers">All customers</Button>}
      />
    )
  }
  if (customer.error.message || !org) {
    return (
      <>
        <PageHeader title="Customer" back={back} />
        <ErrorNotice error={customer.error} onRetry={customer.reload} />
      </>
    )
  }

  const can = orgPermissions(org, true)
  const actions = (
    <>
      {org.status === 'active' ? (
        <Button outline onClick={() => setDialog('suspend')}>
          Suspend
        </Button>
      ) : (
        <Button loading={setStatus.loading} onClick={() => void setStatus.run('active')}>
          {org.status === 'pending' ? 'Approve' : 'Restore'}
        </Button>
      )}
      <Button outline onClick={() => setDialog('limits')}>
        Change plan
      </Button>
      {billing !== 'off' && (
        <Button outline onClick={() => setDialog('credits')}>
          Add credits
        </Button>
      )}
    </>
  )

  return (
    <>
      <PageHeader
        back={back}
        title={org.name}
        meta={<OrgStatusBadge status={org.status} reason={org.status_reason} />}
        description={`${org.slug} · ${org.plan?.name ?? 'No'} plan`}
        actions={actions}
      />
      <PageTabs label="Customer views" tabs={tabs} current={tab} className="mb-8" />

      {tab === 'overview' && <CustomerOverview org={org} onEdit={() => setDialog('limits')} />}
      {tab === 'sandboxes' && <CustomerSandboxes org={org} />}
      {tab === 'keys' && <CustomerKeys org={org} />}
      {tab === 'members' && (
        <>
          <div className="mb-4 flex justify-end">
            <Button outline onClick={() => setDialog('member')}>
              Add member
            </Button>
          </div>
          <MembersSection
            org={org}
            can={can}
            addOpen={dialog === 'member'}
            onAddClose={() => setDialog(null)}
            onLeft={() => navigate('/admin/customers')}
          />
        </>
      )}
      {tab === 'credits' && <CustomerCredits org={org} />}

      <LimitsDialog open={dialog === 'limits'} org={org} onClose={() => setDialog(null)} onSaved={changed} />
      <AddCreditsDialog open={dialog === 'credits'} org={org} onClose={() => setDialog(null)} onAdded={changed} />
      <SuspendDialog org={dialog === 'suspend' ? org : null} onClose={() => setDialog(null)} onSuspended={changed} />
    </>
  )
}

function CustomerOverview({ org, onEdit }: { org: api.Organization; onEdit: () => void }) {
  const { currency } = useInstance()
  const limits = useLoad(() => api.getLimits(org.slug), [org.slug])
  const usage = useLoad(() => api.getUsage({ org: org.slug, from: daysAgo(30), granularity: 'day' }), [org.slug])
  const overridden = (key: keyof api.Limits) => org.overrides?.[key] != null

  return (
    <>
      <div className="flex items-end justify-between gap-4">
        <Subheading>Limits</Subheading>
        <Button plain onClick={onEdit}>
          Edit
        </Button>
      </div>
      <DescriptionList className="mt-2">
        <DescriptionTerm>Sandboxes at once</DescriptionTerm>
        <DescriptionDetails>
          {formatLimit(org.limits.sandboxes)}
          {overridden('sandboxes') && ' (override)'}
        </DescriptionDetails>
        <DescriptionTerm>vCPUs</DescriptionTerm>
        <DescriptionDetails>
          {formatLimit(org.limits.vcpus)}
          {overridden('vcpus') && ' (override)'}
        </DescriptionDetails>
        <DescriptionTerm>Memory</DescriptionTerm>
        <DescriptionDetails>
          {formatLimit(org.limits.memory_mb, formatMemory)}
          {overridden('memory_mb') && ' (override)'}
        </DescriptionDetails>
      </DescriptionList>

      <Subheading className="mt-12 mb-6">In use now</Subheading>
      {limits.data ? (
        <LimitMeters report={limits.data} />
      ) : limits.error.message ? (
        <ErrorNotice error={limits.error} onRetry={limits.reload} />
      ) : (
        <ListSkeleton label="Loading limits" rows={1} />
      )}

      <Subheading className="mt-12 mb-6">Usage, last 30 days</Subheading>
      <UsageSummary usage={usage} currency={currency} onRetry={usage.reload} />
    </>
  )
}

function CustomerSandboxes({ org }: { org: api.Organization }) {
  const sandboxes = useLoad(() => api.listSandboxes({ org: org.slug }), [org.slug], { every: 10_000 })
  const list = sandboxes.data?.data ?? []
  if (sandboxes.loading && !sandboxes.data) return <ListSkeleton label="Loading sandboxes" rows={3} />
  if (sandboxes.error.message) return <ErrorNotice error={sandboxes.error} onRetry={sandboxes.reload} />
  if (list.length === 0) return <EmptyState title="No sandboxes" description={`${org.name} has no sandboxes right now.`} />
  return <SandboxTable sandboxes={list} canChange org={org.slug} onChanged={sandboxes.refresh} />
}

function CustomerKeys({ org }: { org: api.Organization }) {
  const keys = useLoad(() => api.listSandboxKeys(org.slug), [org.slug])
  const list = keys.data?.data ?? []
  if (keys.loading && !keys.data) return <ListSkeleton label="Loading API keys" rows={3} />
  if (keys.error.message) return <ErrorNotice error={keys.error} onRetry={keys.reload} />
  if (list.length === 0) return <EmptyState title="No API keys" description={`${org.name} hasn’t created a key yet.`} />
  return <SandboxKeyTable keys={list} org={org.slug} canRevoke onRevoked={keys.refresh} />
}

function CustomerCredits({ org }: { org: api.Organization }) {
  const credits = useLoad(() => api.getCredits(org.slug), [org.slug])
  const data = credits.data
  if (credits.loading && !data) return <ListSkeleton label="Loading credits" rows={3} />
  if (credits.error.message || !data) return <ErrorNotice error={credits.error} onRetry={credits.reload} />
  return (
    <>
      <div className="grid gap-8 sm:grid-cols-3">
        <Stat title="Balance" value={formatMoney(data.balance_micros, data.currency)} />
        <Stat title="Spent this month" value={formatMoney(data.month_spend_micros, data.currency)} />
        <Stat title="Monthly credits" value={formatMoney(data.monthly_credits_micros, data.currency)} />
      </div>
      <Subheading className="mt-12 mb-4">Transactions</Subheading>
      <CreditTable credits={data} />
    </>
  )
}

// "" is no override (the plan's limit).
function limitText(value: number | null | undefined): string {
  return value == null ? '' : String(value)
}

function parseLimit(text: string): number | null {
  return text.trim() === '' ? null : Number(text)
}

function LimitsDialog({
  open,
  org,
  onClose,
  onSaved,
}: {
  open: boolean
  org: api.Organization
  onClose: () => void
  onSaved: () => void
}) {
  const count = useOpenCount(open)
  return (
    <Dialog open={open} onClose={onClose} size="lg">
      <LimitsForm key={count} org={org} onClose={onClose} onSaved={onSaved} />
    </Dialog>
  )
}

function LimitsForm({ org, onClose, onSaved }: { org: api.Organization; onClose: () => void; onSaved: () => void }) {
  const plans = useLoad(() => api.listPlans(), [])
  const [plan, setPlan] = useState(org.plan?.slug ?? '')
  const [sandboxes, setSandboxes] = useState(limitText(org.overrides?.sandboxes))
  const [vcpus, setVcpus] = useState(limitText(org.overrides?.vcpus))
  const [memory, setMemory] = useState(limitText(org.overrides?.memory_mb))
  const chosen = plans.data?.data.find((option) => option.slug === plan)

  const save = useAction(
    () => {
      const input: api.OrganizationUpdate = {}
      if (plan && plan !== org.plan?.slug) input.plan = plan
      const fields = [
        ['max_sandboxes', sandboxes, org.overrides?.sandboxes],
        ['max_vcpus', vcpus, org.overrides?.vcpus],
        ['max_memory_mb', memory, org.overrides?.memory_mb],
      ] as const
      for (const [key, text, before] of fields) {
        const value = parseLimit(text)
        if (value !== (before ?? null)) input[key] = value
      }
      return api.updateOrganization(org.slug, input)
    },
    { toError: (failure) => ({ message: failureMessage(failure) }) },
  )

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await save.run()
    if (result.ok) {
      toast.success('Plan and limits saved.')
      onSaved()
      onClose()
    }
  }

  const placeholder = (value: number | null | undefined, format = (v: number) => formatNumber(v)) =>
    chosen ? `Plan: ${formatLimit(value ?? null, format)}` : 'Plan’s limit'

  return (
    <form onSubmit={submit}>
      <DialogTitle>Plan and limits</DialogTitle>
      <DialogDescription>An override replaces the plan’s limit for {org.name} only. Leave it empty to use the plan’s.</DialogDescription>
      <DialogBody>
        <FieldGroup>
          <ErrorNotice error={save.error} />
          <ErrorNotice error={plans.error} onRetry={plans.reload} />
          <Field disabled={save.busy || !plans.data}>
            <Label>Plan</Label>
            <Listbox value={plan} onChange={setPlan}>
              {(plans.data?.data ?? []).map((option) => (
                <ListboxOption key={option.slug} value={option.slug}>
                  {option.name}
                </ListboxOption>
              ))}
            </Listbox>
          </Field>
          <div className="grid gap-4 sm:grid-cols-3">
            <Field disabled={save.busy}>
              <Label>Sandboxes</Label>
              <Input type="number" min={0} step={1} value={sandboxes} placeholder={placeholder(chosen?.max_sandboxes)} onChange={(event) => setSandboxes(event.currentTarget.value)} />
            </Field>
            <Field disabled={save.busy}>
              <Label>vCPUs</Label>
              <Input type="number" min={0} step={1} value={vcpus} placeholder={placeholder(chosen?.max_vcpus)} onChange={(event) => setVcpus(event.currentTarget.value)} />
            </Field>
            <Field disabled={save.busy}>
              <Label>Memory (MB)</Label>
              <Input type="number" min={0} step={1} value={memory} placeholder={placeholder(chosen?.max_memory_mb)} onChange={(event) => setMemory(event.currentTarget.value)} />
            </Field>
          </div>
          <Description>Limits hold across every region together.</Description>
        </FieldGroup>
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={save.busy}>
          Cancel
        </Button>
        <Button type="submit" loading={save.loading}>
          Save
        </Button>
      </DialogActions>
    </form>
  )
}

function AddCreditsDialog({
  open,
  org,
  onClose,
  onAdded,
}: {
  open: boolean
  org: api.Organization
  onClose: () => void
  onAdded: () => void
}) {
  const count = useOpenCount(open)
  return (
    <Dialog open={open} onClose={onClose} size="md">
      <AddCredits key={count} org={org} onClose={onClose} onAdded={onAdded} />
    </Dialog>
  )
}

function AddCredits({ org, onClose, onAdded }: { org: api.Organization; onClose: () => void; onAdded: () => void }) {
  const { currency } = useInstance()
  const [amount, setAmount] = useState('')
  const [note, setNote] = useState('')
  const micros = Math.round(Number(amount) * 1e6)
  const valid = amount.trim() !== '' && Number.isFinite(micros) && micros !== 0
  const add = useAction(() => api.addCredits(org.slug, { amount_micros: micros, note: note.trim() }), {
    toError: (failure) => ({ message: failureMessage(failure) }),
  })

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await add.run()
    if (result.ok) {
      toast.success(`${formatMoney(micros, currency)} ${micros > 0 ? 'added' : 'removed'}.`)
      onAdded()
      onClose()
    }
  }

  return (
    <form onSubmit={submit}>
      <DialogTitle>Add credits</DialogTitle>
      <DialogDescription>A negative amount removes credits from {org.name}.</DialogDescription>
      <DialogBody>
        <FieldGroup>
          <ErrorNotice error={add.error} />
          <Field disabled={add.busy}>
            <Label>Amount{currency ? ` (${currency})` : ''}</Label>
            <Input type="number" step="0.01" required autoFocus value={amount} placeholder="25.00" onChange={(event) => setAmount(event.currentTarget.value)} />
          </Field>
          <Field disabled={add.busy}>
            <Label>Note</Label>
            <Input value={note} placeholder="Invoice 1042" onChange={(event) => setNote(event.currentTarget.value)} />
            <Description>The customer sees it on their Billing page.</Description>
          </Field>
        </FieldGroup>
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={add.busy}>
          Cancel
        </Button>
        <Button type="submit" loading={add.loading} disabled={!valid} disabledReason="Enter an amount">
          Add credits
        </Button>
      </DialogActions>
    </form>
  )
}
