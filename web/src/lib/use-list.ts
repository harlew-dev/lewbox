import { useSearchParams } from 'react-router'
import { cleanSearch } from '@harlew-dev/ui/lib/pagination'
import { listHref, parseList, writeList, type ListSpec, type ListState } from './list-query'

// The URL is a list's state. Every change is a navigation, so Back steps
// through the filters and pages. Search replaces the entry instead, and waits
// for the SearchField's 300 ms debounce.
export function useListState(spec: ListSpec) {
  const [params, setParams] = useSearchParams()
  const state = parseList(params, spec)

  function write(next: ListState, replace = false) {
    setParams(writeList(next, spec, params), { preventScrollReset: true, replace })
  }

  function setPage(page: number) {
    write({ ...state, page })
  }

  function setSort(sort: string) {
    write({ ...state, page: 1, sort })
  }

  function setSearch(q: string) {
    write({ ...state, page: 1, q: cleanSearch(q) }, true)
  }

  function setFilter(key: string, values: string[]) {
    const filters = { ...state.filters }
    if (values.length > 0) filters[key] = values
    else delete filters[key]
    write({ ...state, page: 1, filters })
  }

  function removeFilter(key: string) {
    setFilter(key, [])
  }

  function clearFilters() {
    write({ ...state, page: 1, filters: {} })
  }

  // The "no matches" reset: the search as well as the filters.
  function clearAll() {
    write({ ...state, page: 1, q: '', filters: {} })
  }

  function href(next: number | Partial<ListState>) {
    return listHref(
      typeof next === 'number' ? { ...state, page: next } : { ...state, ...next },
      spec,
      params,
    )
  }

  // The query string alone, for a useLoad cache key.
  function key(): string {
    return writeList(state, spec).toString()
  }

  return {
    state,
    params,
    setPage,
    setSort,
    setSearch,
    setFilter,
    removeFilter,
    clearFilters,
    clearAll,
    href,
    key,
  }
}
