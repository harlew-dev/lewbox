import type { Instance, Me, SessionResponse } from '../../types/auth'
import { request, setCsrfToken } from './client'

// keepCsrfToken stores the new session's CSRF token from a sign-in answer.
function keepCsrfToken(session: SessionResponse): SessionResponse {
  setCsrfToken(session.csrf_token)
  return session
}

export function me(): Promise<Me> {
  return request<Me>('/v0/profile', { authRedirect: false }).then((me) => {
    setCsrfToken(me.csrf_token)
    return me
  })
}

export function signIn(email: string, password: string): Promise<SessionResponse> {
  return request<SessionResponse>('/v0/auth/session', {
    method: 'POST',
    body: { email, password },
    authRedirect: false,
  }).then(keepCsrfToken)
}

export function signUp(input: {
  email: string
  password: string
  setup_token?: string
}): Promise<SessionResponse> {
  return request<SessionResponse>('/v0/auth/signup', {
    method: 'POST',
    body: input,
    authRedirect: false,
  }).then(keepCsrfToken)
}

export function instance(): Promise<Instance> {
  return request<Instance>('/v0/instance', { authRedirect: false })
}

export function signOut(): Promise<{ ok: boolean }> {
  return request<{ ok: boolean }>('/v0/auth/logout', {
    method: 'POST',
    authRedirect: false,
  }).finally(() => setCsrfToken(''))
}
