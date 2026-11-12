import {
  Avatar,
  Button,
  ConfirmationDialog,
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownMenu,
  EmptyState,
  ErrorNotice,
  Link,
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
import { useState } from 'react'
import { useSearchParams } from 'react-router'
import * as api from '../../lib/api'
import { formatDate, formatNumber } from '../../lib/format'
import { orgInitials } from '../../lib/organizations'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'
import { useRefreshPending } from '../../components/console-layout'
import { OrgStatusBadge } from '../organizations/roles'
import { NewWorkspaceDialog } from './new-workspace'
import { CreateSetupLinkDialog, SetupLinkBadge } from './setup-links'
import { SuspendDialog } from './suspend-dialog'

const tabs = [
  { id: 'all', label: 'All' },
  { id: 'pending', label: 'Pending' },
  { id: 'suspended', label: 'Suspended' },
  { id: 'setup', label: 'Setup links' },
] as const

const empty = {
  all: { title: 'No workspaces yet', description: 'Add one with New workspace. Self sign-ups show here too.' },
  pending: { title: 'No one is waiting', description: 'Sign-ups wait here for approval when sign-up needs it.' },
  suspended: { title: 'No suspended workspaces', description: 'Workspaces you suspend show here.' },
  setup: { title: 'No setup links', description: 'Create one to onboard a customer with a plan and a region.' },
}

export function WorkspacesPage() {
  useDocumentTitle('Workspaces')
  const [searchParams] = useSearchParams()
  const tab = tabFrom(searchParams, tabs)
  const [createOpen, setCreateOpen] = useState(false)
  const [createLinkOpen, setCreateLinkOpen] = useState(false)
  const [suspend, setSuspend] = useState<api.Organization | null>(null)
  const [revoke, setRevoke] = useState<api.SetupLink | null>(null)
  const refreshPending = useRefreshPending()
  const workspaces = useLoad(
    () =>
      tab === 'setup'
        ? Promise.resolve(null)
        : api.listOrganizations({ status: tab === 'all' ? undefined : tab }),
    [tab],
  )
  const links = useLoad(() => (tab === 'setup' ? api.listSetupLinks() : Promise.resolve(null)), [tab])
  const running = useLoad(
    () => (tab === 'setup' ? Promise.resolve(null) : api.listSandboxes({ status: 'started' })),
    [tab],
  )
  const list = workspaces.data?.data ?? []
  const linkList = links.data?.data ?? []

  const runningBy = new Map<string, number>()
  for (const sandbox of running.data?.data ?? []) {
    const slug = sandbox.organization?.slug
    if (slug) runningBy.set(slug, (runningBy.get(slug) ?? 0) + 1)
  }

  function changed() {
    workspaces.refresh()
    refreshPending()
  }

  async function setStatus(org: api.Organization, status: api.OrgStatus) {
    try {
      await api.updateOrganization(org.slug, { status })
      toast.success(
        status === 'active' && org.status === 'pending' ? `${org.name} approved.` : `${org.name} restored.`,
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
          tab === 'setup' ? (
            <Button onClick={() => setCreateLinkOpen(true)}>Create setup link</Button>
          ) : (
            <Button onClick={() => setCreateOpen(true)}>New workspace</Button>
          )
        }
      />
      <PageTabs label="Workspace views" tabs={tabs} current={tab} className="mb-8" />

      {tab === 'setup' ? (
        links.loading && !links.data ? (
          <ListSkeleton label="Loading setup links" rows={4} />
        ) : links.error.message ? (
          <ErrorNotice error={links.error} onRetry={links.reload} />
        ) : linkList.length === 0 ? (
          <EmptyState {...empty.setup} />
        ) : (
          <Table>
            <TableHead>
              <TableRow>
                <TableHeader>Note</TableHeader>
                <TableHeader>Cluster</TableHeader>
                <TableHeader>Plan</TableHeader>
                <TableHeader>Status</TableHeader>
                <TableHeader>Claimed by</TableHeader>
                <TableHeader>Workspace</TableHeader>
                <TableHeader>Expires</TableHeader>
                <TableHeader className="w-0">
                  <span className="sr-only">Actions</span>
                </TableHeader>
              </TableRow>
            </TableHead>
            <TableBody>
              {linkList.map((link) => (
                <TableRow key={link.id}>
                  <TableCell className="font-medium">{link.note || '—'}</TableCell>
                  <TableCell className="text-zinc-500 dark:text-zinc-400">{link.region.name}</TableCell>
                  <TableCell className="text-zinc-500 dark:text-zinc-400">{link.plan.name}</TableCell>
                  <TableCell>
                    <SetupLinkBadge status={link.status} />
                  </TableCell>
                  <TableCell className="text-zinc-500 dark:text-zinc-400">{link.claimed_by ?? '—'}</TableCell>
                  <TableCell>
                    {link.workspace ? (
                      <Link href={`/admin/workspaces/${encodeURIComponent(link.workspace.slug)}`}>
                        {link.workspace.name}
                      </Link>
                    ) : (
                      <span className="text-zinc-500 dark:text-zinc-400">—</span>
                    )}
                  </TableCell>
                  <TableCell className="text-zinc-500 dark:text-zinc-400">{formatDate(link.expires_at)}</TableCell>
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
              ))}
            </TableBody>
          </Table>
        )
      ) : workspaces.loading && !workspaces.data ? (
        <ListSkeleton label="Loading workspaces" rows={4} />
      ) : workspaces.error.message ? (
        <ErrorNotice error={workspaces.error} onRetry={workspaces.reload} />
      ) : list.length === 0 ? (
        <EmptyState {...empty[tab]} />
      ) : (
        <Table>
          <TableHead>
            <TableRow>
              <TableHeader>Name</TableHeader>
              <TableHeader>Cluster</TableHeader>
              <TableHeader>Plan</TableHeader>
              <TableHeader>Status</TableHeader>
              <TableHeader className="text-right">Running</TableHeader>
              <TableHeader className="text-right">vCPU-h (30 d)</TableHeader>
              <TableHeader className="w-0">
                <span className="sr-only">Actions</span>
              </TableHeader>
            </TableRow>
          </TableHead>
          <TableBody>
            {list.map((org) => (
              <TableRow
                key={org.id}
                href={`/admin/workspaces/${encodeURIComponent(org.slug)}`}
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
                <TableCell className="text-zinc-500 dark:text-zinc-400">{org.region?.name ?? '—'}</TableCell>
                <TableCell className="text-zinc-500 dark:text-zinc-400">{org.region?.plan.name ?? '—'}</TableCell>
                <TableCell>
                  <OrgStatusBadge status={org.status} />
                </TableCell>
                <TableCell className="text-right tabular-nums">{runningBy.get(org.slug) ?? 0}</TableCell>
                <TableCell className="text-right tabular-nums">
                  {org.vcpu_hours_30d === undefined ? '—' : formatNumber(org.vcpu_hours_30d, 1)}
                </TableCell>
                <TableCell className="w-0 text-right">
                  <div className="relative z-10">
                    <Dropdown>
                      <DropdownButton as={Button} plain aria-label="Workspace actions">
                        <EllipsisHorizontalIcon />
                      </DropdownButton>
                      <DropdownMenu anchor="bottom end">
                        <DropdownItem href={`/admin/workspaces/${encodeURIComponent(org.slug)}`}>
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
            ))}
          </TableBody>
        </Table>
      )}

      <NewWorkspaceDialog open={createOpen} onClose={() => setCreateOpen(false)} onChanged={changed} />
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
