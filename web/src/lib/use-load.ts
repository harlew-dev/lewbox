import { useCallback, useEffect, useRef, useState } from 'react'
import type { ActionError } from '@harlew-dev/ui'
import { ApiError } from './api'
import { useAuth } from './auth'
import { emptyError, entryFor, type CacheEntry } from './load-cache'

export function failureMessage(failure: unknown): string {
  if (failure instanceof ApiError) return failure.message
  if (failure instanceof Error && failure.message) return failure.message
  return 'Something went wrong.'
}

// The field an API error names in details.field, if any.
export function errorField(failure: unknown): string {
  if (!(failure instanceof ApiError)) return ''
  const details = failure.details as { field?: unknown } | undefined
  return typeof details?.field === 'string' ? details.field : ''
}

export interface LoadState<T> {
  data: T | null
  error: ActionError
  failure: unknown
  // Nothing loaded for this key yet.
  loading: boolean
  // The API answered 404.
  missing: boolean
  // Revalidating in the background; the data stays on screen.
  refreshing: boolean
  // The data belongs to the previous key and is about to be replaced.
  stale: boolean
  // Loads again, keeping what is shown if it fails.
  reload: () => void
  // Loads again in the background, keeping what is shown until it answers.
  refresh: () => void
}

let nextErrorKey = 0

function notify(entry: CacheEntry): void {
  for (const listener of entry.listeners) listener()
}

function startLoad(loader: () => Promise<unknown>, entry: CacheEntry): Promise<void> {
  if (entry.promise) return entry.promise
  entry.loading = entry.data === null
  entry.refreshing = entry.data !== null
  entry.loaded = false
  entry.error = emptyError
  entry.failure = null
  entry.missing = false
  notify(entry)

  const promise = loader()
    .then((value) => {
      entry.data = value
      entry.failure = null
      entry.error = emptyError
      entry.missing = false
    })
    .catch((failure: unknown) => {
      // The last answer stays, including when this was the first load for a
      // new key: the caller shows it dimmed with the error beside it.
      nextErrorKey += 1
      entry.failure = failure
      entry.error = { message: failureMessage(failure), key: nextErrorKey }
      entry.missing = failure instanceof ApiError && failure.status === 404
    })
    .finally(() => {
      entry.loading = false
      entry.refreshing = false
      entry.loaded = true
      entry.promise = null
      notify(entry)
    })
  entry.promise = promise
  return promise
}

interface View<T> {
  data: T | null
  // The [user, key] the data belongs to.
  dataKey: string | null
  error: ActionError
  failure: unknown
  loading: boolean
  missing: boolean
  refreshing: boolean
}

// A null key fetches nothing and shows no skeleton.
function idleView<T>(): View<T> {
  return {
    data: null,
    dataKey: null,
    error: emptyError,
    failure: null,
    loading: false,
    missing: false,
    refreshing: false,
  }
}

function userOf(cacheKey: string | null): string {
  return cacheKey?.split('\u0000')[0] ?? ''
}

function viewFrom<T>(
  entry: CacheEntry | null,
  previous: View<T> | null,
  cacheKey: string | null,
  userKey: string,
): View<T> {
  const keep =
    entry?.data == null &&
    previous !== null &&
    previous.data !== null &&
    userOf(previous.dataKey) === userKey
  const data = entry?.data != null ? (entry.data as T) : keep ? previous.data : null
  const dataKey = entry?.data != null ? cacheKey : keep ? previous.dataKey : null
  return {
    data,
    dataKey,
    error: entry?.error ?? emptyError,
    failure: entry?.failure ?? null,
    // A key with no answer yet (and no failure) is loading; a failed first
    // load shows its error instead. Data on screen never shows a skeleton.
    loading:
      data === null &&
      (entry === null || entry.loading || (!entry.loaded && entry.error.message === '')),
    missing: entry?.missing ?? false,
    refreshing: entry?.refreshing ?? false,
  }
}

// `key` is the cache key: it must include everything the loader reads (the
// workspace, an ID, a filter). The signed-in user is added here. A null key
// fetches nothing.
//
// `every` polls every so many milliseconds, or when it is a function, the
// number the current answer asks for (0 stops polling). Poll only while
// something on the page is pending.
export function useLoad<T>(
  key: string | null,
  loader: () => Promise<T>,
  { every }: { every?: number | ((data: T | null) => number) } = {},
): LoadState<T> {
  const { user } = useAuth()
  const userKey = user?.id ?? 'guest'
  const cacheKey = key === null ? null : `${userKey}\u0000${key}`
  const latest = useRef(loader)
  latest.current = loader

  const [view, setView] = useState<View<T>>(() =>
    cacheKey === null ? idleView() : viewFrom(entryFor(cacheKey), null, cacheKey, userKey),
  )

  useEffect(() => {
    if (cacheKey === null) {
      setView(idleView())
      return
    }
    const entry = entryFor(cacheKey)
    const sync = () => setView((current) => viewFrom(entry, current, cacheKey, userKey))
    sync()
    entry.listeners.add(sync)
    // Revalidate on mount: a cached answer shows at once, then refreshes.
    void startLoad(latest.current, entry)
    return () => {
      entry.listeners.delete(sync)
    }
  }, [cacheKey, userKey])

  const refresh = useCallback(() => {
    if (cacheKey === null) return
    void startLoad(latest.current, entryFor(cacheKey))
  }, [cacheKey])

  const reload = useCallback(() => {
    if (cacheKey === null) return
    const entry = entryFor(cacheKey)
    // Retry from scratch when nothing is shown; keep the data otherwise.
    entry.loaded = false
    void startLoad(latest.current, entry)
  }, [cacheKey])

  // Refresh when the tab comes back, as stale data is worse than a fetch.
  useEffect(() => {
    if (cacheKey === null) return
    const check = () => {
      const entry = entryFor(cacheKey)
      if (!entry.promise) void startLoad(latest.current, entry)
    }
    const onVisible = () => {
      if (document.visibilityState === 'visible') check()
    }
    window.addEventListener('focus', check)
    window.addEventListener('online', check)
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      window.removeEventListener('focus', check)
      window.removeEventListener('online', check)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [cacheKey])

  const stale = view.data !== null && view.dataKey !== cacheKey
  const interval = typeof every === 'function' ? (stale ? 0 : every(view.data)) : (every ?? 0)
  const poll = useCallback(() => refresh(), [refresh])

  useEffect(() => {
    if (!interval) return
    const timer = window.setInterval(poll, interval)
    return () => window.clearInterval(timer)
  }, [interval, poll])

  return {
    data: view.data,
    error: view.error,
    failure: view.failure,
    loading: view.loading,
    missing: view.missing,
    refreshing: view.refreshing,
    stale,
    reload,
    refresh,
  }
}
