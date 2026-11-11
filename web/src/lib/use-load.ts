import { useCallback, useEffect, useState, type DependencyList } from 'react'
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
  reload: () => void
}

let nextErrorKey = 0

export function useLoad<T>(loader: () => Promise<T>, deps: DependencyList): LoadState<T> {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<ActionError>({ message: '', key: 0 })
  const [failure, setFailure] = useState<unknown>(null)
  const [loading, setLoading] = useState(true)
  const [tick, setTick] = useState(0)

  const reload = useCallback(() => setTick((value) => value + 1), [])

  useEffect(() => {
    let cancelled = false
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

  return { data, error, failure, loading, reload }
}
