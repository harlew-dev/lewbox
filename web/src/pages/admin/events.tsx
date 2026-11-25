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
  TextLink,
} from "@harlew-dev/ui";
import { ChevronDownIcon, ChevronRightIcon } from "@heroicons/react/16/solid";
import { useEffect, useState, type FormEvent } from "react";
import { useSearchParams } from "react-router";
import * as api from "../../lib/api";
import { relativeTime } from "../../lib/format";
import { routes } from "../../lib/routes";
import { useDocumentTitle } from "../../lib/use-document-title";
import { useLoad } from "../../lib/use-load";
import { one, withParam } from "../../lib/query-params";

const PAGE_SIZE = 50;

const filterKeys = ["actor", "organization", "action", "from", "to"] as const;

// The operator's audit trail: every state change, who made it and from where.
export function EventsPage() {
  useDocumentTitle("Audit trail");
  // The applied filters live in the URL, so a reload, Back or a shared link
  // shows the same events; the form edits a draft until it is submitted.
  const [searchParams, setSearchParams] = useSearchParams();
  const applied: api.EventFilters = {};
  for (const key of filterKeys) {
    const value = one(searchParams, key);
    if (value) applied[key] = value;
  }
  const appliedKey = filterKeys.map((key) => applied[key] ?? "").join("\u0000");
  const [draft, setDraft] = useState<api.EventFilters>(() => ({
    ...applied,
    from: localInput(applied.from),
    to: localInput(applied.to),
  }));
  const page = useLoad(`events\u0000${appliedKey}`, () =>
    api.listEvents({ ...applied, limit: PAGE_SIZE }),
  );

  const [extra, setExtra] = useState<api.AuditEvent[]>([]);
  const [next, setNext] = useState<string | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreError, setMoreError] = useState("");

  useEffect(() => {
    setExtra([]);
    setNext(page.data?.next ?? null);
    setMoreError("");
  }, [page.data]);

  const rows = [...(page.data?.data ?? []), ...extra];

  async function loadMore() {
    if (!next) return;
    setLoadingMore(true);
    setMoreError("");
    try {
      const answer = await api.listEvents({
        ...applied,
        before: next,
        limit: PAGE_SIZE,
      });
      setExtra((current) => [...current, ...answer.data]);
      setNext(answer.next);
    } catch {
      setMoreError("Could not load more events. Try again.");
    } finally {
      setLoadingMore(false);
    }
  }

  function apply(event: FormEvent) {
    event.preventDefault();
    const filters: api.EventFilters = {
      ...draft,
      from: rfc3339(draft.from),
      to: rfc3339(draft.to),
    };
    let params = new URLSearchParams();
    for (const key of filterKeys)
      params = withParam(params, key, filters[key]?.trim() ?? "");
    setSearchParams(params);
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
                value={draft.actor ?? ""}
                onChange={(event) =>
                  setDraft({ ...draft, actor: event.target.value })
                }
                placeholder="account ID or email"
              />
            </Field>
            <Field>
              <Label htmlFor="event-workspace">Workspace</Label>
              <Input
                id="event-workspace"
                value={draft.organization ?? ""}
                onChange={(event) =>
                  setDraft({ ...draft, organization: event.target.value })
                }
                placeholder="workspace ID or slug"
              />
            </Field>
            <Field>
              <Label htmlFor="event-action">Action</Label>
              <Input
                id="event-action"
                value={draft.action ?? ""}
                onChange={(event) =>
                  setDraft({ ...draft, action: event.target.value })
                }
                placeholder="plan.updated"
              />
            </Field>
            <Field>
              <Label htmlFor="event-from">From</Label>
              <Input
                id="event-from"
                type="datetime-local"
                value={draft.from ?? ""}
                onChange={(event) =>
                  setDraft({ ...draft, from: event.target.value })
                }
              />
            </Field>
            <Field>
              <Label htmlFor="event-to">Before</Label>
              <Input
                id="event-to"
                type="datetime-local"
                value={draft.to ?? ""}
                onChange={(event) =>
                  setDraft({ ...draft, to: event.target.value })
                }
              />
            </Field>
          </div>
          <div className="flex items-center gap-3">
            <Button type="submit">Apply filters</Button>
            <Button
              type="button"
              plain
              onClick={() => {
                setDraft({});
                setSearchParams(new URLSearchParams());
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
            <Text className="mt-4 text-red-600 dark:text-red-400">
              {moreError}
            </Text>
          )}
          {next && (
            <div className="mt-4 flex justify-center">
              <Button plain onClick={loadMore} disabled={loadingMore}>
                {loadingMore ? "Loading…" : "Load more"}
              </Button>
            </div>
          )}
        </>
      )}
    </>
  );
}

function EventRow({ event }: { event: api.AuditEvent }) {
  const [open, setOpen] = useState(false);
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
            <span className="font-medium">{event.actor_label || "—"}</span>
            <Badge
              color={
                event.actor_kind === "system" ||
                event.actor_kind === "anonymous"
                  ? "zinc"
                  : "purple"
              }
            >
              {event.actor_kind.replace("_", " ")}
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
              <TextLink
                href={routes.admin.workspace(event.organization_slug)}
              >
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
              <Detail
                label="Full time"
                value={new Date(event.created_at).toISOString()}
              />
              <Detail label="IP" value={event.ip || "—"} />
              <Detail
                label="Request ID"
                value={event.request_id || "—"}
                mono
              />
              <Detail label="Target ID" value={event.target_id || "—"} mono />
              <Detail
                label="Workspace ID"
                value={event.organization_id || "—"}
                mono
              />
              <Detail label="Event ID" value={event.id} mono />
            </dl>
          </TableCell>
        </TableRow>
      )}
    </>
  );
}

function Detail({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div>
      <dt className="text-xs font-medium text-zinc-500 uppercase dark:text-zinc-400">
        {label}
      </dt>
      <dd className={mono ? "font-mono break-all" : "break-words"}>{value}</dd>
    </div>
  );
}

function changesText(
  changes: Record<string, api.EventChange> | undefined,
): string {
  if (!changes) return "";
  return Object.entries(changes)
    .map(
      ([field, change]) =>
        `${field}: ${valueText(change.from)} → ${valueText(change.to)}`,
    )
    .join(", ");
}

// localInput turns an RFC 3339 time from the URL back into the value a
// datetime-local input shows, in the browser's time zone.
function localInput(value: string | undefined): string | undefined {
  if (!value) return undefined;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return undefined;
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

// rfc3339 turns a datetime-local input value into the API's RFC 3339 time.
function rfc3339(local: string | undefined): string | undefined {
  if (!local) return undefined;
  const date = new Date(local);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

function targetText(event: api.AuditEvent): string {
  const name = event.target_name || event.target_id;
  if (!name) return event.target_type;
  return `${event.target_type} · ${name}`;
}

function valueText(value: unknown): string {
  if (value === null || value === undefined || value === "") return "—";
  if (typeof value === "string") return value;
  return JSON.stringify(value);
}
