import type { KeyLifetime } from '../../types/key'
import type {
  NewSandboxKey,
  SandboxKey,
  SandboxKeyList,
  SandboxKeyListQuery,
} from '../../types/sandbox-key'
import { query, ref, request, toPage, type PageBody } from './client'

export async function listSandboxKeys(
  org: string,
  filter: SandboxKeyListQuery = {},
): Promise<SandboxKeyList> {
  return toPage(await request<PageBody<SandboxKey>>(`/v0/sandbox-keys${query(filter)}`, { org }))
}

export function createSandboxKey(
  org: string,
  input: { name: string; region?: string; expires_in: KeyLifetime },
): Promise<NewSandboxKey> {
  return request<NewSandboxKey>('/v0/sandbox-keys', { method: 'POST', body: input, org })
}

export function revokeSandboxKey(org: string, keyRef: string): Promise<SandboxKey> {
  return request<SandboxKey>(`/v0/sandbox-keys/${ref(keyRef)}`, { method: 'DELETE', org })
}
