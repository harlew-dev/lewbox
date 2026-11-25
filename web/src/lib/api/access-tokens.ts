import type { KeyLifetime } from '../../types/key'
import type { AccessToken, AccessTokenList, NewAccessToken } from '../../types/access-token'
import { ref, request } from './client'

export function listAccessTokens(): Promise<AccessTokenList> {
  return request<AccessTokenList>('/v0/access-tokens')
}

// A token that never expires is sent with an empty expires_in. current_password
// is the caller's own password, as fresh proof.
export function createAccessToken(input: {
  name: string
  expires_in: KeyLifetime
  current_password: string
}): Promise<NewAccessToken> {
  const body = { ...input, expires_in: input.expires_in === 'never' ? '' : input.expires_in }
  return request<NewAccessToken>('/v0/access-tokens', { method: 'POST', body })
}

export function revokeAccessToken(tokenRef: string): Promise<AccessToken> {
  return request<AccessToken>(`/v0/access-tokens/${ref(tokenRef)}`, { method: 'DELETE' })
}
