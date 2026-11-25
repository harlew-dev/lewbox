import type { Instance, Me, SessionResponse } from '../../types/auth'
import { request } from './client'

export function me(): Promise<Me> {
  return request<Me>('/v0/profile', { authRedirect: false })
}

export function signIn(email: string, password: string): Promise<SessionResponse> {
  return request<SessionResponse>('/v0/auth/session', {
    method: 'POST',
    body: { email, password },
    authRedirect: false,
  })
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
  })
}

export function instance(): Promise<Instance> {
  return request<Instance>('/v0/instance', { authRedirect: false })
}

export function signOut(): Promise<{ ok: boolean }> {
  return request<{ ok: boolean }>('/v0/auth/logout', {
    method: 'POST',
    authRedirect: false,
  })
}
