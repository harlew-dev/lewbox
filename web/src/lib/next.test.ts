import { describe, expect, it } from 'vitest'
import { safeNext } from './next'

describe('safeNext', () => {
  it('keeps same-site paths, including query strings', () => {
    expect(safeNext('/clusters')).toBe('/clusters')
    expect(safeNext('/nodes?cluster=eu-west')).toBe('/nodes?cluster=eu-west')
    expect(safeNext('/clusters/eu-west#checks')).toBe('/clusters/eu-west#checks')
  })

  it('falls back to the dashboard for anything else', () => {
    expect(safeNext(null)).toBe('/')
    expect(safeNext('')).toBe('/')
    expect(safeNext('https://example.com')).toBe('/')
    expect(safeNext('//example.com')).toBe('/')
    expect(safeNext('/\\example.com')).toBe('/')
    expect(safeNext('clusters')).toBe('/')
    expect(safeNext('/login')).toBe('/')
    expect(safeNext('/login?next=%2Fclusters')).toBe('/')
    expect(safeNext('/login#top')).toBe('/')
    expect(safeNext('/\t/example.com')).toBe('/')
    expect(safeNext('/\n/example.com')).toBe('/')
  })
})
