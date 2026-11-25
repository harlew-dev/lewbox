import type { KeyLifetime } from '../../types/key'
import type { NewSandboxKey, SandboxKey } from '../../types/sandbox-key'
import { ref, request } from './client'

export function listSandboxKeys(org: string): Promise<{ data: SandboxKey[] }> {
  return request<{ data: SandboxKey[] }>('/v0/sandbox-keys', { org })
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
