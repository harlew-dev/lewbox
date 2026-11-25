import { describe, expect, it } from 'vitest'
import {
  isFiltered,
  listHref,
  parseList,
  sortDirection,
  sortHeader,
  toggleSort,
  writeList,
  type ListSpec,
} from './list-query'

const spec: ListSpec = {
  sortKeys: ['name', 'created', 'status'],
  defaultSort: 'name',
  descendingFirst: ['created'],
  filters: {
    status: ['active', 'pending', 'suspended'],
    region: ['eu-west', 'us-east'],
    user: null,
  },
}

describe('parseList', () => {
  it('reads the canonical parameters', () => {
    const state = parseList(
      new URLSearchParams('page=3&q=acme&sort=-created&status=active,pending&region=eu-west'),
      spec,
    )
    expect(state).toEqual({
      page: 3,
      q: 'acme',
      sort: '-created',
      filters: { status: ['active', 'pending'], region: ['eu-west'] },
    })
  })

  it('falls back to defaults and drops unknown or repeated values', () => {
    expect(
      parseList(
        new URLSearchParams('page=0&sort=unknown&status=active,nope,active&region=eu-west&region=us-east'),
        spec,
      ),
    ).toEqual({ page: 1, q: '', sort: 'name', filters: { status: ['active'] } })
  })

  it('keeps values a list cannot validate, and caps multi-values at 50', () => {
    const many = Array.from({ length: 60 }, (_, index) => `value-${index}`).join(',')
    const state = parseList(
      new URLSearchParams(`user=acme&user=globex&region=${many}`),
      { ...spec, filters: { ...spec.filters, region: null } },
    )
    expect(state.filters.user).toBeUndefined()
    expect(state.filters.region).toHaveLength(50)
    expect(state.filters.region?.[0]).toBe('value-0')
  })

  it('cleans search like the API: trim, control characters, 100 characters', () => {
    const state = parseList(new URLSearchParams(`q=${' a\u0007b '.repeat(30)}`), spec)
    expect(state.q.length).toBeLessThanOrEqual(100)
    expect(state.q).not.toContain('\u0007')
    expect(state.q.startsWith('ab')).toBe(true)
  })
})

describe('writeList', () => {
  it('leaves defaults out, writes a fixed order and keeps other parameters', () => {
    const params = writeList(
      {
        page: 1,
        q: '',
        sort: 'name',
        filters: { region: ['us-east'], status: ['pending', 'active'] },
      },
      spec,
      new URLSearchParams('tab=pending'),
    )
    expect(params.toString()).toBe('tab=pending&status=pending%2Cactive&region=us-east')
  })

  it('writes non-default values and clears what no longer applies', () => {
    const base = new URLSearchParams('tab=setup&page=4&q=old&status=active&region=eu-west&user=acme')
    const params = writeList(
      { page: 2, q: 'acme', sort: '-created', filters: { status: ['active'] } },
      spec,
      base,
    )
    expect(params.toString()).toBe('tab=setup&page=2&q=acme&sort=-created&status=active')
    expect(listHref({ page: 1, q: '', sort: 'name', filters: {} }, spec, base)).toBe(
      '?tab=setup',
    )
  })
})

describe('sorting', () => {
  it('reverses the current column and starts another in its natural direction', () => {
    expect(toggleSort('name', 'name')).toBe('-name')
    expect(toggleSort('-name', 'name')).toBe('name')
    expect(toggleSort('name', 'created', 'descending')).toBe('-created')
    expect(toggleSort('-created', 'created', 'descending')).toBe('created')
    expect(sortDirection('-created', 'created')).toBe('descending')
    expect(sortDirection('name', 'created')).toBeUndefined()
  })

  it('links a header to the toggled sort on page 1 with the other parameters kept', () => {
    const state = parseList(new URLSearchParams('page=4&sort=name'), spec)
    const header = sortHeader(state, spec, 'created', new URLSearchParams('tab=all'))
    expect(header.sort).toBeUndefined()
    expect(header.href).toBe('?tab=all&sort=-created')
  })
})

describe('isFiltered', () => {
  it('counts a search or any applied filter', () => {
    expect(isFiltered({ q: '', filters: {} })).toBe(false)
    expect(isFiltered({ q: 'acme', filters: {} })).toBe(true)
    expect(isFiltered({ q: '', filters: { status: ['active'] } })).toBe(true)
  })
})
