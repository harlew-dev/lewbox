import type { RestoredArchive, Snapshot } from '../../types/snapshot'
import { ref, request } from './client'

export function getArchive(org: string, id: string): Promise<Snapshot> {
  return request<Snapshot>(`/v0/archives/${ref(id)}`, { org: org || undefined })
}

export function restoreArchive(org: string, id: string): Promise<RestoredArchive> {
  return request<RestoredArchive>(`/v0/archives/${ref(id)}/restore`, { method: 'POST', org })
}

export function deleteArchive(org: string, id: string): Promise<Snapshot> {
  return request<Snapshot>(`/v0/archives/${ref(id)}`, { method: 'DELETE', org: org || undefined })
}
