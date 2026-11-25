import {
  Badge,
  Button,
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
  Text,
} from '@harlew-dev/ui'
import { useEffect, useState, type FormEvent } from 'react'
import * as api from '../../lib/api'
import { relativeTime } from '../../lib/format'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'

const PAGE_SIZE = 50

// The operator's audit trail: every state change, who made it and from where.
export function EventsPage() {
  useDocumentTitle('Audit trail')
  const [draft, setDraft] = useState<api.EventFilters>({})
  const [applied, setApplied] = useState<api.EventFilters>({})
  const page = useLoad(() => api.listEvents({ ...applied, limit: PAGE_SIZE }), [applied])

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
      const answer = await api.listEvents({ ...applied, before: next, limit: PAGE_SIZE })
      setExtra((current) => [...current, ...answer.data])
      setNext(answer.next)
    } catch {
      setMoreError('Could not load more events. Try again.')
    } finally {
      setLoadingMore(false)
    }
  }

  function apply(event: FormEvent) {
    event.preventDefault()
    setApplied({ ...draft, from: rfc3339(draft.from), to: rfc3339(draft.to) })
  }

  return (
    <>
      <PageHeader
        title="Audit trail"
        description="Every change made in this instance: who made it, what changed and from where. Kept for 90 days by default."
      />

      <form onSubmit={apply} className="mb-6">
        <FieldGroup>
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <Field>
              <Label htmlFor="event-actor">Actor</Label>
              <Input
                id="event-actor"
                value={draft.actor ?? ''}
                onChange={(event) => setDraft({ ...draft, actor: event.target.value })}
                placeholder="account ID or email"
              />
            </Field>
            <Field>
              <Label htmlFor="event-workspace">Workspace</Label>
              <Input
                id="event-workspace"
                value={draft.organization ?? ''}
                onChange={(event) => setDraft({ ...draft, organization: event.target.value })}
                placeholder="workspace ID or slug"
              />
            </Field>
            <Field>
              <Label htmlFor="event-action">Action</Label>
              <Input
                id="event-action"
                value={draft.action ?? ''}
                onChange={(event) => setDraft({ ...draft, action: event.target.value })}
                placeholder="plan.updated"
              />
            </Field>
            <Field>
              <Label htmlFor="event-from">From</Label>
              <Input
                id="event-from"
                type="datetime-local"
                value={draft.from ?? ''}
                onChange={(event) => setDraft({ ...draft, from: event.target.value })}
              />
            </Field>
            <Field>
              <Label htmlFor="event-to">Before</Label>
              <Input
                id="event-to"
                type="datetime-local"
                value={draft.to ?? ''}
                onChange={(event) => setDraft({ ...draft, to: event.target.value })}
              />
            </Field>
          </div>
          <div className="flex items-center gap-3">
            <Button type="submit">Apply filters</Button>
            <Button
              type="button"
              plain
              onClick={() => {
                setDraft({})
                setApplied({})
              }}
            >
              Clear
            </Button>
          </div>
        </FieldGroup>
      </form>

      {page.loading && !page.data ? (
        <ListSkeleton label="Loading audit trail" rows={5} />
      ) : page.error.message ? (
        <ErrorNotice error={page.error} onRetry={page.reload} />
      ) : rows.length === 0 ? (
        <EmptyState
          title="No events"
          description="Changes made through Lewbox appear here. A new instance has none yet."
        />
      ) : (
        <>
          <Table>
            <TableHead>
              <TableRow>
                <TableHeader>Time</TableHeader>
                <TableHeader>Actor</TableHeader>
                <TableHeader>Action</TableHeader>
                <TableHeader>Target</TableHeader>
                <TableHeader>Workspace</TableHeader>
                <TableHeader>IP</TableHeader>
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.map((event) => (
                <TableRow key={event.id}>
                  <TableCell
                    className="whitespace-nowrap text-zinc-500 dark:text-zinc-400"
                    title={new Date(event.created_at).toLocaleString()}
                  >
                    {relativeTime(event.created_at)}
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-medium">{event.actor_label || '—'}</span>
                      <Badge color={event.actor_kind === 'system' || event.actor_kind === 'anonymous' ? 'zinc' : 'purple'}>
                        {event.actor_kind.replace('_', ' ')}
                      </Badge>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Text size="sm" className="font-mono">
                      {event.name}
                    </Text>
                    {event.changes && Object.keys(event.changes).length > 0 && (
                      <Text size="sm" className="mt-1 text-zinc-500 dark:text-zinc-400">
                        {Object.entries(event.changes)
                          .map(([field, change]) => `${field}: ${valueText(change.from)} → ${valueText(change.to)}`)
                          .join(', ')}
                      </Text>
                    )}
                  </TableCell>
                  <TableCell>
                    <Text size="sm">{targetText(event)}</Text>
                    {event.target_id && (
                      <Text size="sm" className="font-mono text-zinc-500 dark:text-zinc-400">
                        {event.target_id}
                      </Text>
                    )}
                  </TableCell>
                  <TableCell className="text-zinc-500 dark:text-zinc-400">{event.organization_id || '—'}</TableCell>
                  <TableCell className="whitespace-nowrap text-zinc-500 dark:text-zinc-400">{event.ip || '—'}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>

          {moreError && <Text className="mt-4 text-red-600 dark:text-red-400">{moreError}</Text>}
          {next && (
            <div className="mt-4 flex justify-center">
              <Button plain onClick={loadMore} disabled={loadingMore}>
                {loadingMore ? 'Loading…' : 'Load more'}
              </Button>
            </div>
          )}
        </>
      )}
    </>
  )
}

// rfc3339 turns a datetime-local input value into the API's RFC 3339 time.
function rfc3339(local: string | undefined): string | undefined {
  if (!local) return undefined
  const date = new Date(local)
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString()
}

function targetText(event: api.AuditEvent): string {  const name = event.target_name || event.target_id
  if (!name) return event.target_type
  return `${event.target_type} · ${name}`
}

function valueText(value: unknown): string {
  if (value === null || value === undefined || value === '') return '—'
  if (typeof value === 'string') return value
  return JSON.stringify(value)
}
