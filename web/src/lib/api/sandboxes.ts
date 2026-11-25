import type { Sandbox, SandboxList, SandboxListQuery } from '../../types/sandbox'
import { query, ref, request, toPage, type PageBody } from './client'

// The organization goes in the header and, for instance admins who see
// every organization, in ?org= too. Without one an admin sees the fleet.
export async function listSandboxes(filter: SandboxListQuery = {}): Promise<SandboxList> {
  return toPage(
    await request<PageBody<Sandbox>>(`/v0/sandboxes${query({ ...filter })}`, { org: filter.org }),
  )
}

export function stopSandbox(sandbox: Pick<Sandbox, 'id' | 'region'>, org?: string): Promise<Sandbox> {
  return request<Sandbox>(`/v0/sandboxes/${ref(sandbox.id)}/stop${query({ region: sandbox.region })}`, {
    method: 'POST',
    org,
  })
}

export function deleteSandbox(sandbox: Pick<Sandbox, 'id' | 'region'>, org?: string): Promise<Sandbox> {
  return request<Sandbox>(`/v0/sandboxes/${ref(sandbox.id)}${query({ region: sandbox.region })}`, {
    method: 'DELETE',
    org,
  })
}
