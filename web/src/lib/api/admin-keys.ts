import type { KeyLifetime } from '../../types/key'
import type {
  AdminKey,
  AdminKeyList,
  AdminKeyListQuery,
  NewAdminKey,
} from '../../types/admin-key'
import { query, ref, request, toPage, type PageBody } from './client'

export async function listAdminKeys(
  org: string,
  filter: AdminKeyListQuery = {},
): Promise<AdminKeyList> {
  return toPage(await request<PageBody<AdminKey>>(`/v0/admin-keys${query(filter)}`, { org }))
}

// A management key that never expires is sent with an empty expires_in.
// current_password is the caller's own password, as fresh proof.
export function createAdminKey(
  org: string,
  input: { name: string; expires_in: KeyLifetime; current_password: string },
): Promise<NewAdminKey> {
  const body = { ...input, expires_in: input.expires_in === 'never' ? '' : input.expires_in }
  return request<NewAdminKey>('/v0/admin-keys', { method: 'POST', body, org })
}

export function revokeAdminKey(org: string, keyRef: string): Promise<AdminKey> {
  return request<AdminKey>(`/v0/admin-keys/${ref(keyRef)}`, { method: 'DELETE', org })
}
