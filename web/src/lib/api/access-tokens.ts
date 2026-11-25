import type { KeyLifetime } from '../../types/key'
import type {
  AccessToken,
  AccessTokenList,
  AccessTokenListQuery,
  NewAccessToken,
} from '../../types/access-token'
import { query, ref, request, toPage, type PageBody } from './client'

export async function listAccessTokens(
  filter: AccessTokenListQuery = {},
): Promise<AccessTokenList> {
  return toPage(await request<PageBody<AccessToken>>(`/v0/access-tokens${query(filter)}`))
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
