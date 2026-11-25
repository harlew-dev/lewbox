import type { ActionError } from '@harlew-dev/ui'

export const emptyError: ActionError = { message: '', key: 0 }

export interface CacheEntry {
  data: unknown | null
  failure: unknown
  error: ActionError
  missing: boolean
  loading: boolean
  refreshing: boolean
  loaded: boolean
  promise: Promise<void> | null
  listeners: Set<() => void>
}

// One cache for the whole tab, keyed by [signed-in user, caller's key], so a
// page shows the last answer at once when it is revisited. Kept out of
// use-load.ts so test setup can clear it without importing the hook (and the
// auth store the hook reads).
const cache = new Map<string, CacheEntry>()

export function entryFor(cacheKey: string): CacheEntry {
  const known = cache.get(cacheKey)
  if (known) return known
  const entry: CacheEntry = {
    data: null,
    failure: null,
    error: emptyError,
    missing: false,
    loading: true,
    refreshing: false,
    loaded: false,
    promise: null,
    listeners: new Set(),
  }
  cache.set(cacheKey, entry)
  return entry
}

// Only for tests and sign-out: drop every cached answer.
export function clearLoadCache(): void {
  cache.clear()
}
