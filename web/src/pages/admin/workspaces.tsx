import {
  Avatar,
  Button,
  ConfirmationDialog,
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownMenu,
  ErrorNotice,
  Link,
  PageHeader,
  PageTabs,
  PaginatedTable,
  SearchField,
  TableCell,
  TableHeader,
  TableRow,
  Text,
  toast,
  tabFrom,
} from '@harlew-dev/ui'
import {
  ArrowUturnLeftIcon,
  CheckCircleIcon,
  EllipsisHorizontalIcon,
  EyeIcon,
  NoSymbolIcon,
} from '@heroicons/react/16/solid'
import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router'
import * as api from '../../lib/api'
import { FilterBar, type FilterDef } from '../../components/filter-bar'
import { NoMatches, NoRecords, SortHeader } from '../../components/list'
import { formatDate, formatNumber } from '../../lib/format'
import { isFiltered, listQuery, type ListSpec } from '../../lib/list-query'
import { loadUserOptions, useOptionLabels } from '../../lib/list-options'
import { orgInitials } from '../../lib/organizations'
import { useRegions } from '../../lib/regions'
import { routes } from '../../lib/routes'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useListState } from '../../lib/use-list'
import { useLoad } from '../../lib/use-load'
import { useRefreshPending } from '../../components/console-layout'
import { OrgStatusBadge } from '../organizations/roles'
import { NewWorkspaceDialog } from './new-workspace'
import { CreateSetupLinkDialog, SetupLinkBadge } from './setup-links'
import { SuspendDialog } from './suspend-dialog'

const tabs = [
  { id: 'workspaces', label: 'Workspaces' },
  { id: 'setup', label: 'Setup links' },
] as const

const workspaceSpec: ListSpec = {
  sortKeys: ['name', 'created', 'status'],
  defaultSort: 'name',
  descendingFirst: ['created'],
  filters: {
    status: ['pending', 'active', 'suspended'],
    region: null,
    plan: null,
    user: null,
  },
}

const setupSpec: ListSpec = {
  sortKeys: ['created', 'expires'],
  defaultSort: '-created',
  descendingFirst: ['created', 'expires'],
  filters: {
    status: ['unused', 'claimed', 'used', 'expired'],
    plan: null,
    region: null,
  },
}

const statusOptions = [
  { value: 'pending', label: 'Pending' },
  { value: 'active', label: 'Active' },
  { value: 'suspended', label: 'Suspended' },
]

const setupStatusOptions = [
  { value: 'unused', label: 'Unused' },
  { value: 'claimed', label: 'Claimed' },
  { value: 'used', label: 'Used' },
  { value: 'expired', label: 'Expired' },
]

const empty = {
  workspaces: {
    title: 'No workspaces yet',
    description: 'Add one with New workspace. Self sign-ups show here too.',
  },
  setup: {
    title: 'No setup links',
    description: 'Create one to onboard a customer with a plan and a region.',
  },
}

export function WorkspacesPage() {
  useDocumentTitle('Workspaces')
  const [searchParams, setSearchParams] = useSearchParams()
  const legacy = searchParams.get('tab')
  const legacyStatus = legacy === 'pending' || legacy === 'suspended' ? legacy : ''
  const tab = tabFrom(searchParams, tabs)
  const [createOpen, setCreateOpen] = useState(false)
  const [createLinkOpen, setCreateLinkOpen] = useState(false)
  const [suspend, setSuspend] = useState<api.Organization | null>(null)
  const [revoke, setRevoke] = useState<api.SetupLink | null>(null)
  const refreshPending = useRefreshPending()
  const regions = useRegions()
  const plans = useLoad('plans', () => api.listPlans())

  // Old links, such as the sidebar's pending badge, used tabs for a status.
  useEffect(() => {
    if (legacyStatus === '') return
    const next = new URLSearchParams(searchParams)
    next.delete('tab')
    next.set('status', legacyStatus)
    setSearchParams(next, { replace: true, preventScrollReset: true })
  }, [legacyStatus, searchParams, setSearchParams])

  const workspacesList = useListState(workspaceSpec)
  const setupList = useListState(setupSpec)
  const workspacesState = workspacesList.state
  const setupState = setupList.state

  const workspaces = useLoad<api.OrganizationList | null>(
    tab === 'setup' ? null : `organizations:${workspacesList.key()}`,
    () =>
      api.listOrganizations({
        ...listQuery(workspacesState, workspaceSpec),
        status: workspacesState.filters.status as api.OrgStatus[] | undefined,
        region: workspacesState.filters.region,
        plan: workspacesState.filters.plan,
        user: workspacesState.filters.user?.[0],
      }),
  )
  const links = useLoad<api.SetupLinkList | null>(
    tab === 'setup' ? `setup-links:${setupList.key()}` : null,
    () =>
      api.listSetupLinks({
        ...listQuery(setupState, setupSpec),
        status: setupState.filters.status as api.SetupLinkStatus[] | undefined,
        plan: setupState.filters.plan,
        region: setupState.filters.region,
      }),
  )

  const userLabels = useOptionLabels(workspacesState.filters.user ?? [], loadUserOptions)

  const regionOptions = regions.list.map((region) => ({ value: region.slug, label: region.name }))
  const planOptions = (plans.data?.data ?? []).map((plan) => ({ value: plan.slug, label: plan.name }))

  const workspaceFilters: FilterDef[] = [
    { key: 'status', label: 'Status', options: statusOptions },
    { key: 'region', label: 'Region', options: regionOptions },
    { key: 'plan', label: 'Plan', options: planOptions },
    {
      key: 'user',
      label: 'Owner',
      multiple: false,
      load: loadUserOptions,
      selectedLabels: userLabels,
    },
  ]
  const setupFilters: FilterDef[] = [
    { key: 'status', label: 'Status', options: setupStatusOptions },
    { key: 'plan', label: 'Plan', options: planOptions },
    { key: 'region', label: 'Region', options: regionOptions },
  ]

  function changed() {
    workspaces.refresh()
    refreshPending()
  }

  async function setStatus(org: api.Organization, status: api.OrgStatus) {
    try {
      await api.updateOrganization(org.slug, { status })
      toast.success(
        status === 'active' && org.status === 'pending'
          ? `${org.name} approved.`
          : `${org.name} restored.`,
      )
      changed()
    } catch (failure) {
      toast.error(failure instanceof Error ? failure.message : 'The workspace could not be changed.')
    }
  }

  return (
    <>
      <PageHeader
        title="Workspaces"
        actions={
          <div className="flex flex-wrap items-center gap-3">
            <Button outline onClick={() => setCreateLinkOpen(true)}>
              Create setup link
            </Button>
            <Button onClick={() => setCreateOpen(true)}>New workspace</Button>
          </div>
        }
      />
      <PageTabs label="Workspace views" tabs={tabs} current={tab} className="mb-8" />

      {tab === 'setup' ? (
        <div className="space-y-4">
          <FilterBar
            filters={setupFilters}
            values={setupState.filters}
            onApply={setupList.setFilter}
            onRemove={setupList.removeFilter}
            onClear={setupList.clearFilters}
            leading={
              <SearchField
                label="Search setup links"
                value={setupState.q}
                onSearch={setupList.setSearch}
              />
            }
          />
          {links.error.message && links.data && (
            <ErrorNotice error={links.error} onRetry={links.reload} />
          )}
          {links.error.message && !links.data ? (
            <ErrorNotice error={links.error} onRetry={links.reload} />
          ) : (
            <PaginatedTable
              page={links.data ?? undefined}
              loading={links.loading || links.stale}
              label="Setup links"
              href={setupList.href}
              rowKey={(link) => link.id}
              head={
                <TableRow>
                  <TableHeader>Note</TableHeader>
                  <TableHeader>Region</TableHeader>
                  <TableHeader>Plan</TableHeader>
                  <TableHeader>Status</TableHeader>
                  <TableHeader>Claimed by</TableHeader>
                  <TableHeader>Workspace</TableHeader>
                  <SortHeader
                    state={setupState}
                    spec={setupSpec}
                    column="expires"
                    base={setupList.params}
                  >
                    Expires
                  </SortHeader>
                  <TableHeader className="w-0">
                    <span className="sr-only">Actions</span>
                  </TableHeader>
                </TableRow>
              }
              row={(link) => (
                <TableRow key={link.id}>
                  <TableCell className="font-medium">{link.note || '—'}</TableCell>
                  <TableCell className="text-zinc-500 dark:text-zinc-400">
                    {link.region.name}
                  </TableCell>
                  <TableCell className="text-zinc-500 dark:text-zinc-400">
                    {link.plan.name}
                  </TableCell>
                  <TableCell>
                    <SetupLinkBadge status={link.status} />
                  </TableCell>
                  <TableCell className="text-zinc-500 dark:text-zinc-400">
                    {link.claimed_by ?? '—'}
                  </TableCell>
                  <TableCell>
                    {link.workspace ? (
                      <Link href={routes.admin.workspace(link.workspace.slug)}>
                        {link.workspace.name}
                      </Link>
                    ) : (
                      <span className="text-zinc-500 dark:text-zinc-400">—</span>
                    )}
                  </TableCell>
                  <TableCell className="text-zinc-500 dark:text-zinc-400">
                    {formatDate(link.expires_at)}
                  </TableCell>
                  <TableCell className="w-0 text-right">
                    {link.status !== 'used' && (
                      <div className="relative z-10">
                        <Dropdown>
                          <DropdownButton as={Button} plain aria-label="Setup link actions">
                            <EllipsisHorizontalIcon />
                          </DropdownButton>
                          <DropdownMenu anchor="bottom end">
                            <DropdownItem variant="danger" onClick={() => setRevoke(link)}>
                              <NoSymbolIcon data-slot="icon" />
                              Revoke
                            </DropdownItem>
                          </DropdownMenu>
                        </Dropdown>
                      </div>
                    )}
                  </TableCell>
                </TableRow>
              )}
              empty={
                isFiltered(setupState) ? (
                  <NoMatches label="setup links" onClear={setupList.clearAll} />
                ) : (
                  <NoRecords {...empty.setup} />
                )
              }
            />
          )}
        </div>
      ) : (
        <div className="space-y-4">
          <FilterBar
            filters={workspaceFilters}
            values={workspacesState.filters}
            onApply={workspacesList.setFilter}
            onRemove={workspacesList.removeFilter}
            onClear={workspacesList.clearFilters}
            leading={
              <SearchField
                label="Search workspaces"
                value={workspacesState.q}
                onSearch={workspacesList.setSearch}
              />
            }
          />
          {workspaces.error.message && workspaces.data && (
            <ErrorNotice error={workspaces.error} onRetry={workspaces.reload} />
          )}
          {workspaces.error.message && !workspaces.data ? (
            <ErrorNotice error={workspaces.error} onRetry={workspaces.reload} />
          ) : (
            <PaginatedTable
              page={workspaces.data ?? undefined}
              loading={workspaces.loading || workspaces.stale}
              label="Workspaces"
              href={workspacesList.href}
              rowKey={(org) => org.id}
              head={
                <TableRow>
                  <SortHeader
                    state={workspacesState}
                    spec={workspaceSpec}
                    column="name"
                    base={workspacesList.params}
                  >
                    Name
                  </SortHeader>
                  <TableHeader>Region</TableHeader>
                  <TableHeader>Plan</TableHeader>
                  <SortHeader
                    state={workspacesState}
                    spec={workspaceSpec}
                    column="status"
                    base={workspacesList.params}
                  >
                    Status
                  </SortHeader>
                  <TableHeader className="text-right">Running</TableHeader>
                  <TableHeader className="text-right">vCPU-h (30 d)</TableHeader>
                  <TableHeader className="w-0">
                    <span className="sr-only">Actions</span>
                  </TableHeader>
                </TableRow>
              }
              row={(org) => (
                <TableRow
                  key={org.id}
                  href={routes.admin.workspace(org.slug)}
                  title={`Open ${org.name}`}
                >
                  <TableCell>
                    <div className="flex items-center gap-3">
                      <Avatar initials={orgInitials(org.name)} color="zinc" className="size-8" />
                      <div>
                        <div className="font-medium">{org.name}</div>
                        <Text size="sm">{org.slug}</Text>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className="text-zinc-500 dark:text-zinc-400">
                    {org.region?.name ?? '—'}
                  </TableCell>
                  <TableCell className="text-zinc-500 dark:text-zinc-400">
                    {org.region?.plan.name ?? '—'}
                  </TableCell>
                  <TableCell>
                    <OrgStatusBadge status={org.status} />
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{org.running ?? 0}</TableCell>
                  <TableCell className="text-right tabular-nums">
                    {org.vcpu_hours_30d === undefined
                      ? '—'
                      : formatNumber(org.vcpu_hours_30d, 1)}
                  </TableCell>
                  <TableCell className="w-0 text-right">
                    <div className="relative z-10">
                      <Dropdown>
                        <DropdownButton as={Button} plain aria-label="Workspace actions">
                          <EllipsisHorizontalIcon />
                        </DropdownButton>
                        <DropdownMenu anchor="bottom end">
                          <DropdownItem href={routes.admin.workspace(org.slug)}>
                            <EyeIcon data-slot="icon" />
                            View
                          </DropdownItem>
                          {org.status === 'pending' && (
                            <DropdownItem onClick={() => void setStatus(org, 'active')}>
                              <CheckCircleIcon data-slot="icon" />
                              Approve
                            </DropdownItem>
                          )}
                          {org.status === 'active' && (
                            <DropdownItem onClick={() => setSuspend(org)}>
                              <NoSymbolIcon data-slot="icon" />
                              Suspend
                            </DropdownItem>
                          )}
                          {org.status === 'suspended' && (
                            <DropdownItem onClick={() => void setStatus(org, 'active')}>
                              <ArrowUturnLeftIcon data-slot="icon" />
                              Restore
                            </DropdownItem>
                          )}
                        </DropdownMenu>
                      </Dropdown>
                    </div>
                  </TableCell>
                </TableRow>
              )}
              empty={
                isFiltered(workspacesState) ? (
                  <NoMatches label="workspaces" onClear={workspacesList.clearAll} />
                ) : (
                  <NoRecords {...empty.workspaces} />
                )
              }
            />
          )}
        </div>
      )}

      <NewWorkspaceDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onChanged={changed}
      />
      <CreateSetupLinkDialog
        open={createLinkOpen}
        onClose={() => setCreateLinkOpen(false)}
        onCreated={links.refresh}
      />
      <ConfirmationDialog
        open={revoke !== null}
        onClose={() => setRevoke(null)}
        title="Revoke setup link?"
        description="The link stops working immediately. This can’t be undone."
        confirm="Revoke"
        variant="danger"
        onConfirm={async () => {
          if (revoke) await api.revokeSetupLink(revoke.id)
        }}
        onConfirmed={() => {
          toast.success('Setup link revoked.')
          links.refresh()
        }}
      />
      <SuspendDialog
        org={suspend}
        onClose={() => setSuspend(null)}
        onSuspended={() => {
          setSuspend(null)
          changed()
        }}
      />
    </>
  )
}
