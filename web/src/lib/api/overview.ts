import type { Overview } from '../../types/overview'
import { request } from './client'

export function getOverview(): Promise<Overview> {
  return request<Overview>('/v0/overview')
}
