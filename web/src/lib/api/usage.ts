import type { Usage } from '../../types/usage'
import { query, request } from './client'

export function getUsage(filter: {
  from?: string
  to?: string
  granularity?: 'hour' | 'day'
  org?: string
}): Promise<Usage> {
  return request<Usage>(`/v0/usage${query(filter)}`, { org: filter.org })
}
