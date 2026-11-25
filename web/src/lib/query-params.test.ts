import { describe, expect, it } from 'vitest'
import { one, oneOf, withParam } from './query-params'

describe('query params', () => {
  it('reads a single value and treats a repeated key as absent', () => {
    expect(one(new URLSearchParams('status=started'), 'status')).toBe('started')
    expect(one(new URLSearchParams('status=started&status=stopped'), 'status')).toBe('')
    expect(one(new URLSearchParams(), 'status')).toBe('')
  })

  it('falls back to the default for an unknown value', () => {
    const allowed = ['started', 'stopped']
    expect(oneOf(new URLSearchParams('status=stopped'), 'status', allowed, 'started')).toBe('stopped')
    expect(oneOf(new URLSearchParams('status=destroyed'), 'status', allowed, 'started')).toBe('started')
    expect(oneOf(new URLSearchParams('status=started&status=stopped'), 'status', allowed, 'started')).toBe(
      'started',
    )
  })

  it('writes values and removes the default so one state has one URL', () => {
    const current = new URLSearchParams('tab=snapshots')
    expect(withParam(current, 'status', 'stopped').toString()).toBe('tab=snapshots&status=stopped')
    expect(withParam(current, 'status', '').toString()).toBe('tab=snapshots')
    expect(current.toString()).toBe('tab=snapshots')
  })
})
