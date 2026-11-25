import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Popover } from '@base-ui/react/popover'
import {
  ChevronDownIcon,
  MagnifyingGlassIcon,
  PlusCircleIcon,
  XCircleIcon,
  XMarkIcon,
} from '@heroicons/react/16/solid'
import {
  Button,
  Checkbox,
  ErrorMessage,
  Field,
  Input,
  InputGroup,
  Label,
  RadioGroup,
  Tooltip,
  cx,
  type Page,
} from '@harlew-dev/ui'
import { cleanSearch } from '@harlew-dev/ui/lib/pagination'
import { popoverPanel } from '@harlew-dev/ui/lib/transitions'

// The filter bar of a list: one row of badges, each opening a popover with its
// options (or a date range), plus Clear filters. The page owns the applied
// values (usually the URL); this file knows nothing about routes or the API, so
// it can move into @harlew-dev/ui unchanged.

export type FilterOption = { value: string; label: string }

// A date filter's preset, e.g. { value: '7d', label: 'Last 7 days' }. The page
// maps the value it gets back to its own from/to parameters.
export type FilterRangePreset = { value: string; label: string }

type FilterBase = {
  key: string
  label: string
  // Labels for values that aren't loaded (a loader filter's applied values),
  // so a badge can name them: value → label.
  selectedLabels?: Record<string, string>
}

export type FilterDef =
  | (FilterBase & {
      kind?: 'options'
      // Several values (checkboxes) unless false (radios, one value).
      multiple?: boolean
    } & (
        | { options: FilterOption[]; load?: never }
        | {
            load: (query: string, page: number) => Promise<Page<FilterOption>>
            options?: never
          }
      ))
  | (FilterBase & { kind: 'range'; presets?: FilterRangePreset[] })

// The applied values per filter key. An empty or missing list is not applied.
// Range values are [preset] for a preset, or [from, to] as YYYY-MM-DD dates,
// with '' for a bound that wasn't given.
export type FilterValues = Record<string, string[] | undefined>

export type FilterBarProps = {
  filters: FilterDef[]
  values: FilterValues
  // Receives the filter's key and its new values; an empty list removes it.
  onApply: (key: string, values: string[]) => void
  onRemove: (key: string) => void
  onClear: () => void
  // The page's table search, before the badges.
  leading?: ReactNode
}

export function FilterBar({
  filters,
  values,
  onApply,
  onRemove,
  onClear,
  leading,
}: FilterBarProps) {
  const anyApplied = filters.some((def) => (values[def.key]?.length ?? 0) > 0)
  return (
    // On phones the search sits above the badges, so the badge row keeps the
    // full width; from sm up they share one row.
    <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
      {leading && <div className="min-w-0 sm:w-64 sm:shrink-0">{leading}</div>}
      <div className="flex min-w-0 flex-1 items-center gap-3">
        <div className="-mx-1 flex min-w-0 flex-1 items-center gap-2 overflow-x-auto overflow-y-hidden px-1 py-1">
          {filters.map((def) => (
            <FilterBarBadge
              key={def.key}
              def={def}
              applied={values[def.key] ?? []}
              onApply={onApply}
              onRemove={onRemove}
            />
          ))}
        </div>
        {anyApplied && (
          <button
            type="button"
            onClick={onClear}
            className="flex shrink-0 items-center gap-1 rounded-md px-1.5 py-1 text-sm/5 font-medium whitespace-nowrap text-zinc-600 hover:text-zinc-950 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:text-zinc-400 dark:hover:text-white"
          >
            <XMarkIcon aria-hidden="true" className="size-4" />
            Clear filters
          </button>
        )}
      </div>
    </div>
  )
}

function FilterBarBadge({
  def,
  applied,
  onApply,
  onRemove,
}: {
  def: FilterDef
  applied: string[]
  onApply: (key: string, values: string[]) => void
  onRemove: (key: string) => void
}) {
  const trigger = useRef<HTMLButtonElement>(null)
  const [open, setOpen] = useState(false)
  const isApplied = applied.length > 0
  const summary = isApplied ? summaryOf(def, applied) : null
  // The trigger outlives the applied state (same element, other contents), so
  // focus stays on this badge after its × removes the filter.
  const remove = () => {
    onRemove(def.key)
    trigger.current?.focus()
  }
  const apply = (next: string[]) => {
    onApply(def.key, next)
    setOpen(false)
  }
  return (
    <span
      className={cx(
        'inline-flex flex-none items-center gap-1 rounded-full border',
        isApplied
          ? 'border-zinc-950/15 bg-white py-0.5 pr-1.5 pl-1 text-zinc-950 shadow-xs dark:border-white/15 dark:bg-zinc-800 dark:text-white'
          : 'border-dashed border-zinc-950/25 px-1 py-0.5 text-zinc-600 dark:border-white/25 dark:text-zinc-300',
      )}
    >
      {isApplied && (
        <button
          key="remove"
          type="button"
          aria-label={`Remove ${def.label} filter`}
          onClick={remove}
          className="relative grid size-5 shrink-0 place-items-center rounded-full text-zinc-400 hover:text-zinc-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:text-zinc-500 dark:hover:text-zinc-200"
        >
          <XCircleIcon aria-hidden="true" className="size-4" />
          <span
            aria-hidden="true"
            className="absolute top-1/2 left-1/2 size-[max(100%,2.75rem)] -translate-x-1/2 -translate-y-1/2 pointer-fine:hidden"
          />
        </button>
      )}
      <Popover.Root key="popover" open={open} onOpenChange={setOpen}>
        <Tooltip content={summary?.full ?? ''} disabled={!summary}>
          <Popover.Trigger
            ref={trigger}
            aria-haspopup="dialog"
            className={cx(
              'flex items-center gap-1.5 rounded-full py-1 pr-0.5 pl-1.5 text-sm/5 font-medium whitespace-nowrap',
              'hover:bg-zinc-950/5 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:hover:bg-white/10',
            )}
          >
            {summary ? (
              <>
                <span>{def.label}</span>
                <span aria-hidden="true" className="text-zinc-300 dark:text-zinc-600">
                  │
                </span>
                <span className="max-w-48 truncate">{summary.text}</span>
                <ChevronDownIcon
                  aria-hidden="true"
                  className="size-4 shrink-0 text-zinc-400 dark:text-zinc-500"
                />
              </>
            ) : (
              <>
                <PlusCircleIcon
                  aria-hidden="true"
                  className="size-4 shrink-0 text-zinc-400 dark:text-zinc-500"
                />
                <span>{def.label}</span>
              </>
            )}
          </Popover.Trigger>
        </Tooltip>
        <Popover.Portal>
          <Popover.Positioner
            side="bottom"
            align="start"
            sideOffset={6}
            collisionPadding={8}
            className="z-[60] outline-hidden"
          >
            <Popover.Popup
              className={cx(
                popoverPanel,
                'flex max-h-[min(70vh,32rem)] w-[calc(100vw-2rem)] max-w-[22rem] flex-col rounded-xl bg-white p-4 shadow-lg ring-1 ring-zinc-950/10 outline-none dark:bg-zinc-800 dark:ring-white/10 dark:ring-inset',
              )}
            >
              <FilterPopover def={def} applied={applied} onApply={apply} />
            </Popover.Popup>
          </Popover.Positioner>
        </Popover.Portal>
      </Popover.Root>
    </span>
  )
}

// One popover's contents: options to tick, or a date range. It mounts when the
// popover opens, so every opening starts from the applied values.
function FilterPopover({
  def,
  applied,
  onApply,
}: {
  def: FilterDef
  applied: string[]
  onApply: (values: string[]) => void
}) {
  return def.kind === 'range' ? (
    <RangeForm def={def} applied={applied} onApply={onApply} />
  ) : (
    <OptionsForm def={def} applied={applied} onApply={onApply} />
  )
}

const dateFormat = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  timeZone: 'UTC',
})

// "21 Mar 2026" for YYYY-MM-DD, in UTC so a browser's zone can't move a day.
function dateLabel(value: string): string {
  const time = Date.parse(`${value}T00:00:00Z`)
  return Number.isNaN(time) ? value : dateFormat.format(time)
}

function valueLabel(def: FilterDef, value: string): string {
  if (def.selectedLabels?.[value]) return def.selectedLabels[value]
  if (def.kind === 'range') {
    return def.presets?.find((preset) => preset.value === value)?.label ?? value
  }
  return def.options?.find((option) => option.value === value)?.label ?? value
}

// What the badge writes: one value by name, several as a count. `full` is the
// tooltip's text, where a shortened label is complete again.
function summaryOf(def: FilterDef, applied: string[]): { text: string; full: string } | null {
  if (def.kind === 'range') {
    if (applied.length === 1) {
      const preset = def.presets?.find((p) => p.value === applied[0])
      if (preset) return { text: preset.label, full: preset.label }
    }
    const [from = '', to = ''] = applied
    if (from && to) {
      const range = `${dateLabel(from)} – ${dateLabel(to)}`
      return { text: range, full: range }
    }
    if (from)
      return {
        text: `From ${dateLabel(from)}`,
        full: `From ${dateLabel(from)}`,
      }
    if (to) return { text: `Until ${dateLabel(to)}`, full: `Until ${dateLabel(to)}` }
    return null
  }
  const labels = applied.map((value) => valueLabel(def, value))
  if (labels.length === 1) {
    const label = labels[0] ?? ''
    return { text: label, full: label }
  }
  return { text: `${labels.length} selected`, full: labels.join(', ') }
}

function SectionHeading({ children }: { children: ReactNode }) {
  return (
    <h3 className="text-xs font-semibold tracking-wide text-zinc-500 uppercase dark:text-zinc-400">
      {children}
    </h3>
  )
}

function OptionsForm({
  def,
  applied,
  onApply,
}: {
  def: Extract<FilterDef, { kind?: 'options' }>
  applied: string[]
  onApply: (values: string[]) => void
}) {
  const multiple = def.multiple ?? true
  const fixed = def.options
  const load = def.load
  const [draft, setDraft] = useState<string[]>(() => (multiple ? applied : applied.slice(0, 1)))
  const [query, setQuery] = useState('')
  const [debounced, setDebounced] = useState('')
  const [options, setOptions] = useState<FilterOption[]>(fixed ?? [])
  const [page, setPage] = useState(1)
  const [totalPages, setTotalPages] = useState(1)
  const [status, setStatus] = useState<'idle' | 'loading' | 'error'>(load ? 'loading' : 'idle')
  const [more, setMore] = useState<'idle' | 'loading' | 'error'>('idle')
  const [attempt, setAttempt] = useState(0)
  const generation = useRef(0)
  // The loader is normally an inline arrow; read the newest one without
  // reloading on every parent render.
  const loader = useRef(load)
  loader.current = load
  const searchable = Boolean(load) || (fixed?.length ?? 0) > 10

  useEffect(() => {
    if (!searchable) return
    const timer = window.setTimeout(() => setDebounced(cleanSearch(query)), 300)
    return () => window.clearTimeout(timer)
  }, [query, searchable])

  // The first page for the search, and Retry for it (`attempt`).
  useEffect(() => {
    const load = loader.current
    if (!load) return
    let cancelled = false
    const current = ++generation.current
    setStatus('loading')
    setMore('idle')
    load(debounced, 1)
      .then((next) => {
        if (cancelled || generation.current !== current) return
        setOptions(next.data)
        setPage(1)
        setTotalPages(next.pages.total)
        setStatus('idle')
      })
      .catch(() => {
        if (cancelled || generation.current !== current) return
        setStatus('error')
      })
    return () => {
      cancelled = true
    }
  }, [debounced, attempt])

  const loadMore = async () => {
    const load = loader.current
    if (!load || more === 'loading') return
    const current = generation.current
    setMore('loading')
    try {
      const next = await load(debounced, page + 1)
      if (generation.current !== current) return
      setOptions((loaded) => {
        const seen = new Set(loaded.map((option) => option.value))
        return [...loaded, ...next.data.filter((option) => !seen.has(option.value))]
      })
      setPage((p) => p + 1)
      setTotalPages(next.pages.total)
      setMore('idle')
    } catch {
      if (generation.current !== current) return
      setMore('error')
    }
  }

  const label = (value: string) =>
    def.selectedLabels?.[value] ??
    options.find((option) => option.value === value)?.label ??
    fixed?.find((option) => option.value === value)?.label ??
    value

  const toggle = (value: string, checked: boolean) => {
    setDraft((current) => {
      if (multiple)
        return checked
          ? [...current.filter((v) => v !== value), value]
          : current.filter((v) => v !== value)
      return checked ? [value] : []
    })
  }

  const selectedSet = new Set(draft)
  const selected = draft.map((value) => ({ value, label: label(value) }))
  // Already-selected values are listed first, under their own heading, and
  // never twice.
  // A loader's options are already the server's matches for the search (which
  // may match more than the label); only a fixed list is searched here.
  const term = load ? '' : debounced.toLowerCase()
  const others = options.filter(
    (option) =>
      !selectedSet.has(option.value) && (!term || option.label.toLowerCase().includes(term)),
  )

  return (
    <>
      <Popover.Title className="text-base font-semibold text-zinc-950 dark:text-white">
        Filter by {def.label}
      </Popover.Title>
      {searchable && (
        <div className="mt-3">
          <InputGroup>
            <MagnifyingGlassIcon data-slot="icon" />
            <Input
              type="search"
              aria-label={`Search ${def.label}`}
              placeholder="Search…"
              maxLength={100}
              value={query}
              onChange={(event) => setQuery(event.currentTarget.value)}
            />
          </InputGroup>
        </div>
      )}
      <div className="-mx-1 mt-3 min-h-0 flex-1 overflow-y-auto px-1">
        {status === 'loading' && (
          <p className="py-2 text-sm/6 text-zinc-500 dark:text-zinc-400">Loading…</p>
        )}
        {status === 'error' && (
          <p className="py-2 text-sm/6 text-zinc-500 dark:text-zinc-400">
            Couldn't load options.{' '}
            <button
              type="button"
              onClick={() => setAttempt((n) => n + 1)}
              className="font-medium text-zinc-950 underline decoration-zinc-950/50 hover:decoration-zinc-950 dark:text-white dark:decoration-white/50 dark:hover:decoration-white"
            >
              Retry
            </button>
          </p>
        )}
        {status === 'idle' && (
          <div className="space-y-3 pb-1">
            {selected.length > 0 && (
              <div className="space-y-3">
                <SectionHeading>Selected</SectionHeading>
                {multiple ? (
                  selected.map((option) => (
                    <Checkbox
                      key={option.value}
                      checked
                      onChange={(checked) => toggle(option.value, checked)}
                    >
                      {option.label}
                    </Checkbox>
                  ))
                ) : (
                  <RadioGroup
                    label={`${def.label}, selected`}
                    value={draft[0] ?? ''}
                    onChange={(value) => setDraft([value])}
                    options={selected}
                  />
                )}
              </div>
            )}
            {others.length > 0 &&
              (multiple ? (
                others.map((option) => (
                  <Checkbox
                    key={option.value}
                    checked={selectedSet.has(option.value)}
                    onChange={(checked) => toggle(option.value, checked)}
                  >
                    {option.label}
                  </Checkbox>
                ))
              ) : (
                <RadioGroup
                  label={def.label}
                  value={draft[0] ?? ''}
                  onChange={(value) => setDraft([value])}
                  options={others}
                />
              ))}
            {selected.length === 0 && others.length === 0 && (
              <p className="py-2 text-sm/6 text-zinc-500 dark:text-zinc-400">
                No matching options.
              </p>
            )}
          </div>
        )}
        {load && page < totalPages && status === 'idle' && (
          <div className="pt-2">
            {more === 'error' ? (
              <button
                type="button"
                onClick={loadMore}
                className="text-sm/6 font-medium text-red-600 underline decoration-red-600/50 hover:decoration-red-600 dark:text-red-400 dark:decoration-red-400/50 dark:hover:decoration-red-400"
              >
                Couldn't load more · Retry
              </button>
            ) : (
              <Button plain className="w-full" onClick={loadMore} disabled={more === 'loading'}>
                {more === 'loading' ? 'Loading…' : 'Load more'}
              </Button>
            )}
          </div>
        )}
      </div>
      <div className="mt-4 border-t border-zinc-950/5 pt-4 dark:border-white/10">
        <Button className="w-full" onClick={() => onApply(draft)}>
          Apply
        </Button>
      </div>
    </>
  )
}

const customRange = '__custom__'

function RangeForm({
  def,
  applied,
  onApply,
}: {
  def: Extract<FilterDef, { kind: 'range' }>
  applied: string[]
  onApply: (values: string[]) => void
}) {
  const presets = def.presets ?? []
  const [mode, setMode] = useState(() => {
    if (applied.length === 1 && presets.some((p) => p.value === applied[0])) return applied[0] ?? ''
    if (applied.length === 1) return customRange
    if (applied.length >= 2) return customRange
    return ''
  })
  const [from, setFrom] = useState(() => (mode === customRange ? (applied[0] ?? '') : ''))
  const [to, setTo] = useState(() => (mode === customRange ? (applied[1] ?? '') : ''))
  const [error, setError] = useState<{
    field: 'from' | 'to'
    message: string
  } | null>(null)

  const choose = (value: string) => {
    setMode(value)
    setError(null)
  }

  const apply = () => {
    if (mode === customRange) {
      if (!from && !to) {
        setError({ field: 'from', message: 'Choose a date.' })
        return
      }
      if (from && to && from > to) {
        setError({ field: 'to', message: 'The end comes before the start.' })
        return
      }
      onApply([from, to])
      return
    }
    onApply(mode ? [mode] : [])
  }

  return (
    <>
      <Popover.Title className="text-base font-semibold text-zinc-950 dark:text-white">
        Filter by {def.label}
      </Popover.Title>
      <div className="-mx-1 mt-3 min-h-0 flex-1 overflow-y-auto px-1">
        <RadioGroup
          label={def.label}
          value={mode}
          onChange={choose}
          options={[...presets, { value: customRange, label: 'Custom range' }]}
        />
        {mode === customRange && (
          <div className="mt-3 grid gap-3 sm:grid-cols-2">
            <Field>
              <Label>From</Label>
              <Input
                type="date"
                value={from}
                invalid={error?.field === 'from'}
                max={to || undefined}
                onChange={(event) => {
                  setFrom(event.currentTarget.value)
                  setError(null)
                }}
              />
              {error?.field === 'from' && <ErrorMessage role="alert">{error.message}</ErrorMessage>}
            </Field>
            <Field>
              <Label>To</Label>
              <Input
                type="date"
                value={to}
                invalid={error?.field === 'to'}
                min={from || undefined}
                onChange={(event) => {
                  setTo(event.currentTarget.value)
                  setError(null)
                }}
              />
              {error?.field === 'to' && <ErrorMessage role="alert">{error.message}</ErrorMessage>}
            </Field>
          </div>
        )}
      </div>
      <div className="mt-4 border-t border-zinc-950/5 pt-4 dark:border-white/10">
        <Button className="w-full" onClick={apply}>
          Apply
        </Button>
      </div>
    </>
  )
}
