import {
  Badge,
  Button,
  EmptyState,
  ErrorNotice,
  ListSkeleton,
  PageHeader,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Text,
  TextLink,
} from '@harlew-dev/ui'
import { ChevronDownIcon, ChevronRightIcon } from '@heroicons/react/16/solid'
import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router'
import * as api from '../../lib/api'
import { FilterBar, type FilterDef } from '../../components/filter-bar'
import { NoMatches } from '../../components/list'
import { relativeTime } from '../../lib/format'
import {
  loadOrganizationOptions,
  loadUserOptions,
  useOptionLabels,
} from '../../lib/list-options'
import { routes } from '../../lib/routes'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'
import { one } from '../../lib/query-params'

const PAGE_SIZE = 50

// Every event Lewbox records. The API accepts these as a comma list.
const eventActions = [
  'access_token.created',
  'access_token.revoked',
  'admin_key.created',
  'admin_key.revoked',
  'archive.deleted',
  'archive.restored',
  'cluster.created',
  'cluster.deleted',
  'cluster.updated',
  'dns_profile.created',
  'dns_profile.credentials_read',
  'dns_profile.deleted',
  'dns_profile.updated',
  'join_token.created',
  'member.created',
  'member.deleted',
  'member.updated',
  'node.created',
  'node.deleted',
  'plan.created',
  'plan.deleted',
  'plan.updated',
  'rate_limit.refused',
  'sandbox.deleted',
  'sandbox.stopped',
  'sandbox_key.created',
  'sandbox_key.revoked',
  'session.login_failed',
  'settings.updated',
  'setup_link.created',
  'setup_link.revoked',
  'snapshot.deleted',
  'storage_profile.created',
  'storage_profile.credentials_read',
  'storage_profile.deleted',
  'storage_profile.updated',
  'subscription.created',
  'subscription.deleted',
  'subscription.restored',
  'subscription.updated',
  'user.created',
  'user.deleted',
  'user.fresh_proof_failed',
  'user.password_changed',
  'user.restored',
  'user.suspended',
  'workspace.created',
  'workspace.restored',
  'workspace.suspended',
  'workspace.updated',
].map((name) => ({ value: name, label: name }))

const rangePresets = [
  { value: '24h', label: 'Last 24 hours' },
  { value: '7d', label: 'Last 7 days' },
  { value: '30d', label: 'Last 30 days' },
  { value: '90d', label: 'Last 90 days' },
]

const rangeDays: Record<string, number> = { '24h': 1, '7d': 7, '30d': 30, '90d': 90 }

function listParam(params: URLSearchParams, key: string): string[] {
  const raw = one(params, key)
  return raw === '' ? [] : raw.split(',').filter((value) => value !== '')
}

// The range badge: a preset value, or two `YYYY-MM-DD` dates joined by `..`.
function parseRange(raw: string): string[] {
  if (raw === '') return []
  return raw.includes('..') ? raw.split('..') : [raw]
}

function rangeFilters(values: string[]): { from?: string; to?: string } {
  if (values.length === 1 && rangeDays[values[0] ?? ''] !== undefined) {
    const days = rangeDays[values[0] ?? ''] ?? 0
    return { from: new Date(Date.now() - days * 86_400_000).toISOString() }
  }
  const [from = '', to = ''] = values
  return {
    from: from ? new Date(`${from}T00:00:00Z`).toISOString() : undefined,
    to: to ? new Date(`${to}T23:59:59.999Z`).toISOString() : undefined,
  }
}

function writeRange(values: string[]): string {
  if (values.length === 0) return ''
  if (values.length === 1) return values[0] ?? ''
  return values.join('..')
}

function applied(state: {
  actor: string[]
  organization: string[]
  action: string[]
  range: string[]
}): boolean {
  return (
    state.actor.length > 0 ||
    state.organization.length > 0 ||
    state.action.length > 0 ||
    state.range.length > 0
  )
}

// The operator's audit trail: every state change, who made it and from where.
export function EventsPage() {
  useDocumentTitle('Audit trail')
  // The applied filters live in the URL, so a reload, Back or a shared link
  // shows the same events.
  const [searchParams, setSearchParams] = useSearchParams()
  const values = {
    actor: listParam(searchParams, 'actor'),
    organization: listParam(searchParams, 'organization'),
    action: listParam(searchParams, 'action'),
    range: parseRange(one(searchParams, 'range')),
  }
  const filters = {
    ...rangeFilters(values.range),
    actor: values.actor,
    organization: values.organization,
    action: values.action,
    limit: PAGE_SIZE,
  }
  const filterKey = [
    values.actor.join(','),
    values.organization.join(','),
    values.action.join(','),
    values.range.join('..'),
  ].join('\u0000')
  const page = useLoad(`events\u0000${filterKey}`, () => api.listEvents(filters))
  const actorLabels = useOptionLabels(values.actor, loadUserOptions)
  const organizationLabels = useOptionLabels(
    values.organization,
    loadOrganizationOptions,
  )

  const [extra, setExtra] = useState<api.AuditEvent[]>([])
  const [next, setNext] = useState<string | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const [moreError, setMoreError] = useState('')

  useEffect(() => {
    setExtra([])
    setNext(page.data?.next ?? null)
    setMoreError('')
  }, [page.data])

  const rows = [...(page.data?.data ?? []), ...extra]

  async function loadMore() {
    if (!next) return
    setLoadingMore(true)
    setMoreError('')
    try {
      const answer = await api.listEvents({
        ...filters,
        before: next,
        limit: PAGE_SIZE,
      })
      setExtra((current) => [...current, ...answer.data])
      setNext(answer.next)
    } catch {
      setMoreError('Could not load more events. Try again.')
    } finally {
      setLoadingMore(false)
    }
  }

  function apply(key: string, next: string[]) {
    const params = new URLSearchParams(searchParams)
    params.delete('before')
    if (next.length === 0) params.delete(key)
    else if (key === 'range') params.set(key, writeRange(next))
    else params.set(key, next.join(','))
    setSearchParams(params, { preventScrollReset: true })
  }

  function remove(key: string) {
    apply(key, [])
  }

  function clear() {
    const params = new URLSearchParams(searchParams)
    for (const key of ['actor', 'organization', 'action', 'range', 'before']) {
      params.delete(key)
    }
    setSearchParams(params, { preventScrollReset: true })
  }

  const defs: FilterDef[] = [
    {
      key: 'actor',
      label: 'Actor',
      load: loadUserOptions,
      selectedLabels: actorLabels,
    },
    {
      key: 'organization',
      label: 'Workspace',
      load: loadOrganizationOptions,
      selectedLabels: organizationLabels,
    },
    { key: 'action', label: 'Action', options: eventActions },
    { key: 'range', label: 'Time', kind: 'range', presets: rangePresets },
  ]

  return (
    <>
      <PageHeader
        title="Audit trail"
        description="Every change made in this instance: who made it, what changed and from where. Kept for 90 days by default."
      />

      <div className="space-y-4">
        <FilterBar
          filters={defs}
          values={values}
          onApply={apply}
          onRemove={remove}
          onClear={clear}
        />

        {page.loading && !page.data ? (
          <ListSkeleton label="Loading audit trail" rows={5} />
        ) : page.error.message ? (
          <ErrorNotice error={page.error} onRetry={page.reload} />
        ) : rows.length === 0 ? (
          applied(values) ? (
            <NoMatches label="events" onClear={clear} />
          ) : (
            <EmptyState
              title="No events"
              description="Changes made through Lewbox appear here. A new instance has none yet."
            />
          )
        ) : (
          <>
            <Table>
              <TableHead>
                <TableRow>
                  <TableHeader>Time</TableHeader>
                  <TableHeader>Actor</TableHeader>
                  <TableHeader>Action</TableHeader>
                  <TableHeader>Target</TableHeader>
                  <TableHeader className="w-0">
                    <span className="sr-only">Details</span>
                  </TableHeader>
                </TableRow>
              </TableHead>
              <TableBody>
                {rows.map((event) => (
                  <EventRow key={event.id} event={event} />
                ))}
              </TableBody>
            </Table>

            {moreError && (
              <Text className="mt-4 text-red-600 dark:text-red-400">{moreError}</Text>
            )}
            {next && (
              <div className="mt-4 flex justify-center">
                <Button plain onClick={loadMore} disabled={loadingMore}>
                  {loadingMore ? 'Loading…' : 'Load more'}
                </Button>
              </div>
            )}
          </>
        )}
      </div>
    </>
  )
}

function EventRow({ event }: { event: api.AuditEvent }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <TableRow>
        <TableCell
          className="whitespace-nowrap text-zinc-500 dark:text-zinc-400"
          title={new Date(event.created_at).toLocaleString()}
        >
          {relativeTime(event.created_at)}
        </TableCell>
        <TableCell>
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{event.actor_label || '—'}</span>
            <Badge
              color={
                event.actor_kind === 'system' || event.actor_kind === 'anonymous'
                  ? 'zinc'
                  : 'purple'
              }
            >
              {event.actor_kind.replace('_', ' ')}
            </Badge>
          </div>
        </TableCell>
        <TableCell>
          <Text size="sm" className="font-mono">
            {event.name}
          </Text>
          {changesText(event.changes) && (
            <Text size="sm" className="mt-1 text-zinc-500 dark:text-zinc-400">
              {changesText(event.changes)}
            </Text>
          )}
        </TableCell>
        <TableCell>
          <Text size="sm">{targetText(event)}</Text>
          {event.organization_slug && (
            <Text size="sm" className="mt-1">
              <TextLink href={routes.admin.workspace(event.organization_slug)}>
                {event.organization_slug}
              </TextLink>
            </Text>
          )}
        </TableCell>
        <TableCell className="w-0 text-right">
          <Button
            plain
            onClick={() => setOpen((value) => !value)}
            aria-expanded={open}
            aria-label={`Details for ${event.name}`}
          >
            {open ? <ChevronDownIcon /> : <ChevronRightIcon />}
          </Button>
        </TableCell>
      </TableRow>
      {open && (
        <TableRow>
          <TableCell colSpan={5} className="bg-zinc-50 dark:bg-white/5">
            <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2 lg:grid-cols-3">
              <Detail label="Full time" value={new Date(event.created_at).toISOString()} />
              <Detail label="IP" value={event.ip || '—'} />
              <Detail label="Request ID" value={event.request_id || '—'} mono />
              <Detail label="Target ID" value={event.target_id || '—'} mono />
              <Detail label="Workspace ID" value={event.organization_id || '—'} mono />
              <Detail label="Event ID" value={event.id} mono />
            </dl>
          </TableCell>
        </TableRow>
      )}
    </>
  )
}

function Detail({
  label,
  value,
  mono = false,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div>
      <dt className="text-xs font-medium text-zinc-500 uppercase dark:text-zinc-400">{label}</dt>
      <dd className={mono ? 'font-mono break-all' : 'break-words'}>{value}</dd>
    </div>
  )
}

function changesText(changes: Record<string, api.EventChange> | undefined): string {
  if (!changes) return ''
  return Object.entries(changes)
    .map(([field, change]) => `${field}: ${valueText(change.from)} → ${valueText(change.to)}`)
    .join(', ')
}

function targetText(event: api.AuditEvent): string {
  const name = event.target_name || event.target_id
  if (!name) return event.target_type
  return `${event.target_type} · ${name}`
}

function valueText(value: unknown): string {
  if (value === null || value === undefined || value === '') return '—'
  if (typeof value === 'string') return value
  return JSON.stringify(value)
}
