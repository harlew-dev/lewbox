import type { KeyLifetime } from '../../types/key'
import type { AdminKey, AdminKeyList, NewAdminKey } from '../../types/admin-key'
import { ref, request } from './client'

export function listAdminKeys(org: string): Promise<AdminKeyList> {
  return request<AdminKeyList>('/v0/admin-keys', { org })
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
