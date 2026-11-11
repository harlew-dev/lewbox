import { useCallback, useEffect, useRef, useState, type DependencyList } from 'react'
import type { ActionError } from '@harlew-dev/ui'
import { ApiError } from './api'

export function failureMessage(failure: unknown): string {
  if (failure instanceof ApiError) return failure.message
  if (failure instanceof Error && failure.message) return failure.message
  return 'Something went wrong.'
}

export interface LoadState<T> {
  data: T | null
  error: ActionError
  failure: unknown
  loading: boolean
  // Loads again from scratch, showing the loading state.
  reload: () => void
  // Loads again in the background, keeping what is shown until it answers.
  refresh: () => void
}

let nextErrorKey = 0

// `every` refreshes in the background every so many milliseconds.
export function useLoad<T>(
  loader: () => Promise<T>,
  deps: DependencyList,
  { every }: { every?: number } = {},
): LoadState<T> {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<ActionError>({ message: '', key: 0 })
  const [failure, setFailure] = useState<unknown>(null)
  const [loading, setLoading] = useState(true)
  const [tick, setTick] = useState(0)

  const reload = useCallback(() => setTick((value) => value + 1), [])
  const latest = useRef(loader)
  latest.current = loader
  const generation = useRef(0)

  useEffect(() => {
    let cancelled = false
    generation.current += 1
    setLoading(true)
    setData(null)
    setError({ message: '', key: 0 })
    setFailure(null)
    loader()
      .then((value) => {
        if (cancelled) return
        setData(value)
        setLoading(false)
      })
      .catch((failure: unknown) => {
        if (cancelled) return
        nextErrorKey += 1
        setFailure(failure)
        setError({ message: failureMessage(failure), key: nextErrorKey })
        setLoading(false)
      })
    return () => {
      cancelled = true
    }
    // The caller owns the dependencies of its loader.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, reload, tick])

  const refresh = useCallback(() => {
    const at = generation.current
    latest
      .current()
      .then((value) => {
        if (generation.current !== at) return
        setData(value)
        setError({ message: '', key: 0 })
        setFailure(null)
      })
      .catch(() => {
        // Keep showing the last answer; the next refresh tries again.
      })
  }, [])

  useEffect(() => {
    if (!every) return
    const timer = window.setInterval(refresh, every)
    return () => window.clearInterval(timer)
  }, [every, refresh])

  // A refresh that answers after the component is gone is ignored.
  useEffect(() => () => void (generation.current += 1), [])

  return { data, error, failure, loading, reload, refresh }
}
