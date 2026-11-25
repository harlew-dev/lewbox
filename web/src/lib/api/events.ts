import type { EventFilters, EventPage } from '../../types/event'
import { query, request } from './client'

export function listEvents(filters: EventFilters = {}): Promise<EventPage> {
  return request<EventPage>(
    `/v0/events${query({
      before: filters.before,
      actor: filters.actor,
      organization: filters.organization,
      action: filters.action,
      from: filters.from,
      to: filters.to,
      limit: filters.limit,
    })}`,
  )
}
