import { act, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { MemoryRouter } from 'react-router'
import { describe, expect, it } from 'vitest'
import type { ListSpec } from './list-query'
import { useListState } from './use-list'

const spec: ListSpec = {
  sortKeys: ['name', 'created'],
  defaultSort: 'name',
  descendingFirst: ['created'],
  filters: { status: ['active', 'pending'] },
}

function at(entry: string) {
  return ({ children }: { children: ReactNode }) => (
    <MemoryRouter initialEntries={[entry]}>{children}</MemoryRouter>
  )
}

describe('useListState', () => {
  it('resets to page 1 when a filter changes, and writes the canonical URL', () => {
    const { result } = renderHook(() => useListState(spec), {
      wrapper: at('/?page=4&status=active'),
    })
    expect(result.current.state.page).toBe(4)

    act(() => result.current.setFilter('status', ['pending']))

    expect(result.current.state.page).toBe(1)
    expect(result.current.params.toString()).toBe('status=pending')
  })

  it('resets to page 1 on search and sort, and keeps the page otherwise', () => {
    const { result } = renderHook(() => useListState(spec), {
      wrapper: at('/?page=4&status=active'),
    })

    act(() => result.current.setSearch('acme'))
    expect(result.current.state).toMatchObject({ page: 1, q: 'acme' })

    act(() => result.current.setPage(3))
    expect(result.current.state.page).toBe(3)

    act(() => result.current.setSort('-created'))
    expect(result.current.state.page).toBe(1)
    expect(result.current.params.toString()).toBe('q=acme&sort=-created&status=active')
  })

  it('clears the filters but keeps the search, and clearAll drops both', () => {
    const { result } = renderHook(() => useListState(spec), {
      wrapper: at('/?q=acme&sort=-created&status=active'),
    })

    act(() => result.current.clearFilters())
    expect(result.current.params.toString()).toBe('q=acme&sort=-created')

    act(() => result.current.clearAll())
    expect(result.current.params.toString()).toBe('sort=-created')
  })
})
