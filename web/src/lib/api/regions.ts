import type { Region } from '../../types/region'
import { request } from './client'

// Instance admins get every ready region. Anyone else gets the regions
// `org` is subscribed to, with the plan and limits there.
export function listRegions(org?: string): Promise<{ data: Region[] }> {
  return request<{ data: Region[] }>('/v0/regions', { org })
}
