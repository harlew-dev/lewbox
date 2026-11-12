import {
  Button,
  ConfirmationDialog,
  Description,
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
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
  Listbox,
  ListboxOption,
  ListSkeleton,
  PageHeader,
  PageTabs,
  Subheading,
  Text,
  tabFrom,
  toast,
  useAction,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { EllipsisHorizontalIcon, PencilIcon, TrashIcon } from '@heroicons/react/16/solid'
import { useState, type FormEvent } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router'
import { useRefreshPending } from '../../components/console-layout'
import { SandboxKeyTable } from '../../components/sandbox-key-table'
import { SandboxTable } from '../../components/sandbox-table'
import { SubscriptionTable } from '../../components/subscription-table'
import { UsageSummary } from '../../components/usage-summary'
import * as api from '../../lib/api'
import { daysAgo, formatLimit, formatMemory, formatNumber } from '../../lib/format'
import { invalidLimitReason, limitText, parseLimit, validLimits } from '../../lib/limits'
import { orgPermissions } from '../../lib/organizations'
import { useRegions, type Regions } from '../../lib/regions'
import { useDocumentTitle } from '../../lib/use-document-title'
import { failureMessage, useLoad } from '../../lib/use-load'
import { MembersSection } from '../organizations/members'
import { OrgStatusBadge } from '../organizations/roles'
import { SuspendDialog } from './suspend-dialog'

const back = { href: '/admin/customers', label: 'Customers' }

const tabs = [
  { id: 'overview', label: 'Overview' },
  { id: 'sandboxes', label: 'Sandboxes' },
  { id: 'keys', label: 'API keys' },
  { id: 'members', label: 'Members' },
] as const

export function CustomerDetailPage() {
  const { org: slug = '' } = useParams()
  const navigate = useNavigate()
  const refreshPending = useRefreshPending()
  const [searchParams] = useSearchParams()
  const customer = useLoad(() => api.getOrganization(slug), [slug])
  const org = customer.data
  useDocumentTitle(org?.name ?? 'Customer')
  const [dialog, setDialog] = useState<'suspend' | 'member' | null>(null)
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
        description={`There is no workspace ${slug}.`}
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
  const actions =
    org.status === 'active' ? (
      <Button outline onClick={() => setDialog('suspend')}>
        Suspend
      </Button>
    ) : (
      <Button loading={setStatus.loading} onClick={() => void setStatus.run('active')}>
        {org.status === 'pending' ? 'Approve' : 'Restore'}
      </Button>
    )

  return (
    <>
      <PageHeader
        back={back}
        title={org.name}
        meta={<OrgStatusBadge status={org.status} />}
        description={org.slug}
        actions={actions}
      />
      <PageTabs label="Customer views" tabs={tabs} current={tab} className="mb-8" />

      {tab === 'overview' && <CustomerOverview org={org} onChanged={changed} />}
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

      <SuspendDialog org={dialog === 'suspend' ? org : null} onClose={() => setDialog(null)} onSuspended={changed} />
    </>
  )
}

function CustomerOverview({ org, onChanged }: { org: api.Organization; onChanged: () => void }) {
  const usage = useLoad(() => api.getUsage({ org: org.slug, from: daysAgo(30), granularity: 'day' }), [org.slug])
  const regions = useRegions()
  return (
    <>
      <RegionPlans org={org} regions={regions} onChanged={onChanged} />
      <Subheading className="mt-14 mb-6">Usage, last 30 days</Subheading>
      <UsageSummary usage={usage} regionName={regions.name} onRetry={usage.reload} />
    </>
  )
}

type Editing = { subscription: api.Subscription | null } | null

// The workspace's plan in each region, with what it uses there now.
function RegionPlans({ org, regions, onChanged }: { org: api.Organization; regions: Regions; onChanged: () => void }) {
  const subscriptions = useLoad(() => api.listSubscriptions(org.slug), [org.slug])
  const limits = useLoad(() => api.getLimits(org.slug), [org.slug])
  const [editing, setEditing] = useState<Editing>(null)
  const [removing, setRemoving] = useState<api.Subscription | null>(null)
  const list = subscriptions.data?.data ?? []
  const reports = new Map((limits.data?.regions ?? []).map((region) => [region.slug, region]))
  const subscribed = new Set(list.map((subscription) => subscription.region.slug))
  const free = regions.list.filter((region) => !subscribed.has(region.slug))

  function saved() {
    subscriptions.refresh()
    limits.refresh()
    onChanged()
  }

  return (
    <>
      <div className="mb-4 flex items-end justify-between gap-4">
        <Subheading>Regions</Subheading>
        <Button
          outline
          onClick={() => setEditing({ subscription: null })}
          disabled={!regions.loaded || free.length === 0}
          disabledReason={regions.loaded ? 'It has a plan in every region' : 'Loading regions'}
        >
          Add region
        </Button>
      </div>
      {subscriptions.loading && !subscriptions.data ? (
        <ListSkeleton label="Loading regions" rows={2} />
      ) : subscriptions.error.message ? (
        <ErrorNotice error={subscriptions.error} onRetry={subscriptions.reload} />
      ) : list.length === 0 ? (
        <Text>No regions. {org.name} can’t run sandboxes until you add one.</Text>
      ) : (
        <SubscriptionTable
          subscriptions={list}
          reports={reports}
          actions={(subscription) => (
            <Dropdown>
              <DropdownButton as={Button} plain aria-label={`Actions for ${subscription.region.name}`}>
                <EllipsisHorizontalIcon />
              </DropdownButton>
              <DropdownMenu anchor="bottom end">
                <DropdownItem onClick={() => setEditing({ subscription })}>
                  <PencilIcon data-slot="icon" />
                  Change plan
                </DropdownItem>
                <DropdownItem variant="danger" onClick={() => setRemoving(subscription)}>
                  <TrashIcon data-slot="icon" />
                  Remove region
                </DropdownItem>
              </DropdownMenu>
            </Dropdown>
          )}
        />
      )}

      <SubscriptionDialog editing={editing} org={org} regions={free} onClose={() => setEditing(null)} onSaved={saved} />
      <RemoveRegionDialog org={org} subscription={removing} onClose={() => setRemoving(null)} onRemoved={saved} />
    </>
  )
}

// Removing a region revokes the workspace's keys there. When a key can't be
// revoked (the error says so), the operator can remove it anyway.
function RemoveRegionDialog({
  org,
  subscription,
  onClose,
  onRemoved,
}: {
  org: api.Organization
  subscription: api.Subscription | null
  onClose: () => void
  onRemoved: () => void
}) {
  const [force, setForce] = useState(false)
  const name = subscription?.region.name ?? 'region'
  return (
    <ConfirmationDialog
      open={subscription !== null}
      onClose={() => {
        setForce(false)
        onClose()
      }}
      title={`Remove ${name}?`}
      description={
        force
          ? `Its API keys in ${name} couldn’t all be revoked. Removing it anyway leaves those keys on the region until it can be reached.`
          : `${org.name}’s API keys in ${name} are revoked and its sandboxes there are stopped. Its sandboxes and usage history stay.`
      }
      confirm={force ? 'Remove anyway' : 'Remove region'}
      variant="danger"
      onConfirm={async () => {
        if (subscription) await api.deleteSubscription(org.slug, subscription.region.slug, force)
      }}
      onError={(failure) => {
        if (failure instanceof api.ApiError && /revoke/i.test(failure.message)) setForce(true)
      }}
      onConfirmed={() => {
        setForce(false)
        toast.success(`${name} removed.`)
        onRemoved()
      }}
    />
  )
}

function SubscriptionDialog({
  editing,
  org,
  regions,
  onClose,
  onSaved,
}: {
  editing: Editing
  org: api.Organization
  regions: api.Region[]
  onClose: () => void
  onSaved: () => void
}) {
  const count = useOpenCount(editing !== null)
  return (
    <Dialog open={editing !== null} onClose={onClose} size="lg">
      {editing && (
        <SubscriptionForm
          key={count}
          subscription={editing.subscription}
          org={org}
          regions={regions}
          onClose={onClose}
          onSaved={onSaved}
        />
      )}
    </Dialog>
  )
}

function SubscriptionForm({
  subscription,
  org,
  regions,
  onClose,
  onSaved,
}: {
  subscription: api.Subscription | null
  org: api.Organization
  regions: api.Region[]
  onClose: () => void
  onSaved: () => void
}) {
  const plans = useLoad(() => api.listPlans(), [])
  const [region, setRegion] = useState(subscription?.region.slug ?? regions[0]?.slug ?? '')
  const [plan, setPlan] = useState(subscription?.plan.slug ?? '')
  const [sandboxes, setSandboxes] = useState(limitText(subscription?.overrides?.sandboxes))
  const [vcpus, setVcpus] = useState(limitText(subscription?.overrides?.vcpus))
  const [memory, setMemory] = useState(limitText(subscription?.overrides?.memory_mb))
  const list = plans.data?.data ?? []
  const chosen = list.find((option) => option.slug === (plan || list.find((p) => p.default)?.slug))
  const planSlug = chosen?.slug ?? ''
  const valid = validLimits(sandboxes, vcpus, memory)

  const save = useAction(
    () =>
      api.setSubscription(org.slug, region, {
        plan: planSlug,
        max_sandboxes: parseLimit(sandboxes),
        max_vcpus: parseLimit(vcpus),
        max_memory_mb: parseLimit(memory),
      }),
    { toError: (failure) => ({ message: failureMessage(failure) }) },
  )

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await save.run()
    if (result.ok) {
      toast.success(subscription ? `${result.value.region.name} saved.` : `${result.value.region.name} added.`)
      onSaved()
      onClose()
    }
  }

  const placeholder = (value: number | null | undefined, format = (v: number) => formatNumber(v)) =>
    chosen ? `Plan: ${formatLimit(value ?? null, format)}` : 'Plan’s limit'

  return (
    <form onSubmit={submit}>
      <DialogTitle>{subscription ? `${subscription.region.name} plan` : 'Add a region'}</DialogTitle>
      <DialogDescription>
        {subscription
          ? `What ${org.name} may run in ${subscription.region.name}.`
          : `${org.name} can create keys and run sandboxes there, up to the plan’s limits.`}
      </DialogDescription>
      <DialogBody>
        <FieldGroup>
          <ErrorNotice error={save.error} />
          <ErrorNotice error={plans.error} onRetry={plans.reload} />
          {!subscription && (
            <Field disabled={save.busy}>
              <Label>Region</Label>
              <Listbox value={region} onChange={setRegion} placeholder="Choose a region">
                {regions.map((option) => (
                  <ListboxOption key={option.slug} value={option.slug}>
                    {option.name}
                  </ListboxOption>
                ))}
              </Listbox>
            </Field>
          )}
          <Field disabled={save.busy || !plans.data}>
            <Label>Plan</Label>
            <Listbox value={planSlug} onChange={setPlan} placeholder="Choose a plan">
              {list.map((option) => (
                <ListboxOption key={option.slug} value={option.slug}>
                  {option.name}
                </ListboxOption>
              ))}
            </Listbox>
          </Field>
          <div className="grid gap-4 sm:grid-cols-3">
            <Field disabled={save.busy}>
              <Label>Sandboxes</Label>
              <Input type="number" min={1} step={1} value={sandboxes} placeholder={placeholder(chosen?.max_sandboxes)} onChange={(event) => setSandboxes(event.currentTarget.value)} />
            </Field>
            <Field disabled={save.busy}>
              <Label>vCPUs</Label>
              <Input type="number" min={1} step={1} value={vcpus} placeholder={placeholder(chosen?.max_vcpus)} onChange={(event) => setVcpus(event.currentTarget.value)} />
            </Field>
            <Field disabled={save.busy}>
              <Label>Memory (MB)</Label>
              <Input type="number" min={1} step={1} value={memory} placeholder={placeholder(chosen?.max_memory_mb, formatMemory)} onChange={(event) => setMemory(event.currentTarget.value)} />
            </Field>
          </div>
          <Description>An override replaces the plan’s limit in this region only. Leave it empty to use the plan’s.</Description>
        </FieldGroup>
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={save.busy}>
          Cancel
        </Button>
        <Button
          type="submit"
          loading={save.loading}
          disabled={region === '' || planSlug === '' || !valid}
          disabledReason={valid ? 'Fill in required fields' : invalidLimitReason}
        >
          {subscription ? 'Save' : 'Add region'}
        </Button>
      </DialogActions>
    </form>
  )
}

function CustomerSandboxes({ org }: { org: api.Organization }) {
  const sandboxes = useLoad(() => api.listSandboxes({ org: org.slug }), [org.slug], { every: 10_000 })
  const regions = useRegions()
  const list = sandboxes.data?.data ?? []
  if (sandboxes.loading && !sandboxes.data) return <ListSkeleton label="Loading sandboxes" rows={3} />
  if (sandboxes.error.message) return <ErrorNotice error={sandboxes.error} onRetry={sandboxes.reload} />
  if (list.length === 0) return <EmptyState title="No sandboxes" description={`${org.name} has no sandboxes right now.`} />
  return <SandboxTable sandboxes={list} regionName={regions.name} canChange org={org.slug} onChanged={sandboxes.refresh} />
}

function CustomerKeys({ org }: { org: api.Organization }) {
  const keys = useLoad(() => api.listSandboxKeys(org.slug), [org.slug])
  const list = keys.data?.data ?? []
  if (keys.loading && !keys.data) return <ListSkeleton label="Loading API keys" rows={3} />
  if (keys.error.message) return <ErrorNotice error={keys.error} onRetry={keys.reload} />
  if (list.length === 0) return <EmptyState title="No API keys" description={`${org.name} hasn’t created a key yet.`} />
  return <SandboxKeyTable keys={list} org={org.slug} canRevoke onRevoked={keys.refresh} />
}
