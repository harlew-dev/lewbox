import type { StorageProfile, StorageProfileInput } from '../../types/storage-profile'
import { ref, request } from './client'

export function listStorageProfiles(): Promise<{ data: StorageProfile[] }> {
  return request<{ data: StorageProfile[] }>('/v0/storage-profiles')
}

export function createStorageProfile(
  input: StorageProfileInput & { name: string; bucket: string; access_key_id: string; secret_access_key: string },
): Promise<StorageProfile> {
  return request<StorageProfile>('/v0/storage-profiles', { method: 'POST', body: { provider: 's3', ...input } })
}

export function updateStorageProfile(profileRef: string, input: StorageProfileInput): Promise<StorageProfile> {
  return request<StorageProfile>(`/v0/storage-profiles/${ref(profileRef)}`, { method: 'PATCH', body: input })
}

export function deleteStorageProfile(profileRef: string): Promise<StorageProfile> {
  return request<StorageProfile>(`/v0/storage-profiles/${ref(profileRef)}`, { method: 'DELETE' })
}

export function testStorageProfile(profileRef: string): Promise<StorageProfile> {
  return request<StorageProfile>(`/v0/storage-profiles/${ref(profileRef)}/check`, { method: 'POST' })
}
