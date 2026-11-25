import { cleanSearch } from '@harlew-dev/ui/lib/pagination'
import { one } from './query-params'

// A list's state lives in the URL as page, q, sort and one comma-separated
// parameter per filter. Parsing is forgiving (a hand-edited URL never causes a
// rejected request); writing leaves defaults out, so each state has one URL.
//
// The API's defaults are page 1, 20 rows, no search and the list's default
// sort. `sort` is a key from the list's allowlist with a `-` prefix for
// descending.

export const MAX_FILTER_VALUES = 50
export const MAX_PAGE = 10_000

export type SortDirection = 'ascending' | 'descending'

export interface ListSpec {
  // The sort keys, without their `-`. Every order ends with the ID server-side.
  sortKeys: readonly string[]
  // The sort a list opens with, such as "name" or "-created".
  defaultSort: string
  // Date and count columns start descending; text starts ascending.
  descendingFirst?: readonly string[]
  // A filter's allowed values, or null when the page can't know them (a
  // workspace or account chosen from a paged list).
  filters?: Record<string, readonly string[] | null>
}

export interface ListState {
  page: number
  q: string
  sort: string
  filters: Record<string, string[]>
}

const LIST_KEYS = ['page', 'q', 'sort'] as const

function baseKey(sort: string): string {
  return sort.startsWith('-') ? sort.slice(1) : sort
}

export function parseList(params: URLSearchParams, spec: ListSpec): ListState {
  const rawPage = Number(one(params, 'page'))
  const page =
    Number.isInteger(rawPage) && rawPage >= 1 && rawPage <= MAX_PAGE ? rawPage : 1
  const q = cleanSearch(one(params, 'q'))
  const rawSort = one(params, 'sort')
  const sort = spec.sortKeys.includes(baseKey(rawSort)) ? rawSort : spec.defaultSort
  const filters: Record<string, string[]> = {}
  for (const [key, allowed] of Object.entries(spec.filters ?? {})) {
    const raw = one(params, key)
    if (raw === '') continue
    const values: string[] = []
    for (const item of raw.split(',')) {
      const value = item.trim()
      if (value === '' || values.includes(value)) continue
      if (allowed !== null && !allowed.includes(value)) continue
      values.push(value)
      if (values.length === MAX_FILTER_VALUES) break
    }
    if (values.length > 0) filters[key] = values
  }
  return { page, q, sort, filters }
}

// The list's parameters, in one fixed order, on top of any other parameters
// the page keeps (`tab`, `org`, …).
export function writeList(
  state: ListState,
  spec: ListSpec,
  base?: URLSearchParams,
): URLSearchParams {
  const next = new URLSearchParams(base)
  const filterKeys = Object.keys(spec.filters ?? {})
  for (const key of [...LIST_KEYS, ...filterKeys]) next.delete(key)
  if (state.page > 1) next.set('page', String(state.page))
  if (state.q !== '') next.set('q', state.q)
  if (state.sort !== spec.defaultSort) next.set('sort', state.sort)
  for (const key of filterKeys) {
    const values = state.filters[key]
    if (values && values.length > 0) next.set(key, values.join(','))
  }
  return next
}

// A relative href for a sortable header or a page link; the browser/router
// keeps the current path.
export function listHref(state: ListState, spec: ListSpec, base?: URLSearchParams): string {
  return `?${writeList(state, spec, base).toString()}`
}

// Clicking the current column reverses it. Another column starts in its
// natural direction.
export function toggleSort(current: string, key: string, first: SortDirection = 'ascending'): string {
  if (baseKey(current) === key) return current.startsWith('-') ? key : `-${key}`
  return first === 'descending' ? `-${key}` : key
}

export function sortDirection(sort: string, key: string): SortDirection | undefined {
  if (baseKey(sort) !== key) return undefined
  return sort.startsWith('-') ? 'descending' : 'ascending'
}

// What to pass as a sortable TableHeader's `sort`, and the href that toggles it.
export function sortHeader(
  state: ListState,
  spec: ListSpec,
  key: string,
  base?: URLSearchParams,
): { href: string; sort: SortDirection | undefined } {
  const first = spec.descendingFirst?.includes(key) ? 'descending' : 'ascending'
  return {
    href: listHref({ ...state, page: 1, sort: toggleSort(state.sort, key, first) }, spec, base),
    sort: sortDirection(state.sort, key),
  }
}

export function isFiltered(state: Pick<ListState, 'q' | 'filters'>): boolean {
  return state.q !== '' || Object.values(state.filters).some((values) => values.length > 0)
}

// The request's parameters, with the API's defaults left out.
export function listQuery(
  state: ListState,
  spec: ListSpec,
): { page?: number; q?: string; sort?: string } {
  return {
    page: state.page > 1 ? state.page : undefined,
    q: state.q === '' ? undefined : state.q,
    sort: state.sort === spec.defaultSort ? undefined : state.sort,
  }
}
