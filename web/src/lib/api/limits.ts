import type { LimitsReport } from '../../types/region'
import { request } from './client'

export function getLimits(org: string): Promise<LimitsReport> {
  return request<LimitsReport>('/v0/limits', { org })
}
