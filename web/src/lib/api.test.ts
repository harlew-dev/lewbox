import { describe, expect, it, vi } from 'vitest'
import { ApiError, me } from './api'

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('api error parsing', () => {
  it('turns an API error body into ApiError', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async () =>
        json(409, {
          error: {
            code: 'conflict',
            message: 'cannot remove the last enabled instance admin',
            details: { field: 'email' },
          },
        }),
      ),
    )

    const failure = await me().catch((error: unknown) => error)
    expect(failure).toBeInstanceOf(ApiError)
    expect(failure).toMatchObject({
      status: 409,
      code: 'conflict',
      message: 'cannot remove the last enabled instance admin',
      details: { field: 'email' },
    })
  })

  it('uses a generic message when the body is not the error shape', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('bad gateway', { status: 502 })))

    await expect(me()).rejects.toMatchObject({
      status: 502,
      code: 'unknown',
      message: 'Request failed with status 502.',
    })
  })

  it('sends the session cookie and parses a user', async () => {
    const user = {
      id: '01a1cb34-659b-74a8-983d-085c0383e0a6',
      email: 'admin@example.com',
      instance_admin: true,
      disabled: false,
      created_at: '2026-11-11T15:22:51.1Z',
    }
    const fetchMock = vi.fn().mockResolvedValue(
      json(200, { user, via: 'session' }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await expect(me()).resolves.toEqual({ user, via: 'session' })
    expect(fetchMock).toHaveBeenCalledWith(
      '/v0/me',
      expect.objectContaining({ credentials: 'same-origin' }),
    )
  })
})
