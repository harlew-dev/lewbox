import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from './api'
import { useLoad } from './use-load'

// A signed-in account without an AuthProvider: the hook only reads the user
// ID from it, and switching it is how the cache-key test changes accounts.
const auth = vi.hoisted(() => ({ user: { id: 'user-a' } as { id: string } | null }))

vi.mock('./auth', () => ({
  useAuth: () => ({ user: auth.user }),
}))

beforeEach(() => {
  auth.user = { id: 'user-a' }
})

describe('useLoad', () => {
  it('keeps the previous data on screen while a new key loads', async () => {
    let release: (value: string) => void = () => {}
    const loader = (key: string) =>
      key === 'b'
        ? new Promise<string>((resolve) => {
            release = resolve
          })
        : Promise.resolve('A')

    const { result, rerender } = renderHook(({ key }) => useLoad(key, () => loader(key)), {
      initialProps: { key: 'a' },
    })
    await waitFor(() => expect(result.current.data).toBe('A'))

    rerender({ key: 'b' })
    await waitFor(() => expect(result.current.stale).toBe(true))
    expect(result.current.data).toBe('A')
    expect(result.current.loading).toBe(false)

    act(() => release('B'))
    await waitFor(() => expect(result.current.data).toBe('B'))
    expect(result.current.stale).toBe(false)
  })

  it('shows a cached answer at once on a revisit, then revalidates', async () => {
    const loader = vi.fn(async () => 'cached')
    const first = renderHook(() => useLoad('revisit', loader))
    await waitFor(() => expect(first.result.current.data).toBe('cached'))
    first.unmount()

    const second = renderHook(() => useLoad('revisit', loader))
    expect(second.result.current.data).toBe('cached')
    await waitFor(() => expect(loader).toHaveBeenCalledTimes(2))
    expect(second.result.current.data).toBe('cached')
  })

  it('keeps the data when a reload fails', async () => {
    let fail = false
    const loader = vi.fn(async () => {
      if (fail) throw new Error('The server did not answer.')
      return 'A'
    })
    const { result } = renderHook(() => useLoad('failure', loader))
    await waitFor(() => expect(result.current.data).toBe('A'))

    fail = true
    act(() => result.current.reload())
    await waitFor(() => expect(result.current.error.message).toBe('The server did not answer.'))
    expect(result.current.data).toBe('A')
    expect(result.current.error.message).toBeTruthy()
  })

  it('reports a 404 as missing instead of a generic failure', async () => {
    const loader = vi.fn(async () => {
      throw new ApiError(404, 'not_found', 'There is no such thing.')
    })
    const { result } = renderHook(() => useLoad('missing', loader))

    await waitFor(() => expect(result.current.missing).toBe(true))
    expect(result.current.data).toBeNull()
    expect(result.current.loading).toBe(false)
  })

  it('never shows one account the data cached for another', async () => {
    const firstLoader = vi.fn(async () => 'A data')
    const first = renderHook(() => useLoad('shared', firstLoader))
    await waitFor(() => expect(first.result.current.data).toBe('A data'))
    first.unmount()

    auth.user = { id: 'user-b' }
    const secondLoader = vi.fn(async () => 'B data')
    const second = renderHook(() => useLoad('shared', secondLoader))
    expect(second.result.current.data).toBeNull()
    expect(second.result.current.loading).toBe(true)

    await waitFor(() => expect(second.result.current.data).toBe('B data'))
    expect(secondLoader).toHaveBeenCalled()
  })

  it('polls only while the answer says something is pending', async () => {
    let pending = true
    const loader = vi.fn(async () => (pending ? 'pending' : 'done'))
    const { result } = renderHook(() =>
      useLoad('poll', loader, { every: (data) => (data === 'pending' ? 20 : 0) }),
    )

    await waitFor(() => expect(loader.mock.calls.length).toBeGreaterThan(1), { timeout: 1000 })

    pending = false
    await waitFor(() => expect(result.current.data).toBe('done'))
    // Let the interval that saw the old answer settle before counting.
    await new Promise((resolve) => setTimeout(resolve, 60))
    const calls = loader.mock.calls.length
    await new Promise((resolve) => setTimeout(resolve, 80))
    expect(loader.mock.calls.length).toBe(calls)
  })
})
