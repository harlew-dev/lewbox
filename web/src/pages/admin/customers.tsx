import {
  Avatar,
  Button,
  EmptyState,
  ErrorNotice,
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
} from '@harlew-dev/ui'
import { useState } from 'react'
import { useSearchParams } from 'react-router'
import * as api from '../../lib/api'
import { formatNumber } from '../../lib/format'
import { orgInitials } from '../../lib/organizations'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'
import { OrgStatusBadge } from '../organizations/roles'
import { NewCustomerDialog } from './new-customer'

const tabs = [
  { id: 'all', label: 'All' },
  { id: 'pending', label: 'Pending' },
  { id: 'suspended', label: 'Suspended' },
] as const

const empty = {
  all: { title: 'No customers yet', description: 'Add one with New customer. Self sign-ups show here too.' },
  pending: { title: 'No one is waiting', description: 'Sign-ups wait here for approval when sign-up needs it.' },
  suspended: { title: 'No suspended customers', description: 'Customers you suspend show here.' },
}

export function CustomersPage() {
  useDocumentTitle('Customers')
  const [searchParams] = useSearchParams()
  const tab = tabFrom(searchParams, tabs)
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
              <TableHeader>Workspace</TableHeader>
              <TableHeader>Regions</TableHeader>
              <TableHeader>Status</TableHeader>
              <TableHeader className="text-right">Running</TableHeader>
              <TableHeader className="text-right">vCPU-hours (30 d)</TableHeader>
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
                <TableCell className="text-zinc-500 dark:text-zinc-400">
                  {org.regions.length === 0
                    ? 'None'
                    : org.regions.map((region) => `${region.name} · ${region.plan.name}`).join(', ')}
                </TableCell>
                <TableCell>
                  <OrgStatusBadge status={org.status} />
                </TableCell>
                <TableCell className="text-right tabular-nums">{runningBy.get(org.slug) ?? 0}</TableCell>
                <TableCell className="text-right tabular-nums">
                  {org.vcpu_hours_30d === undefined ? '—' : formatNumber(org.vcpu_hours_30d, 1)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <NewCustomerDialog open={createOpen} onClose={() => setCreateOpen(false)} onChanged={customers.refresh} />
    </>
  )
}
