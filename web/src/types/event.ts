export interface EventChange {
  from: unknown
  to: unknown
}

// One row of the audit trail: who changed what, when and from where.
export interface AuditEvent {
  id: string
  name: string
  actor_kind: string
  actor_id?: string
  actor_label: string
  target_type: string
  target_id: string
  target_name?: string
  organization_id?: string
  organization_slug?: string
  changes?: Record<string, EventChange>
  ip?: string
  request_id?: string
  created_at: string
}

export interface EventPage {
  data: AuditEvent[]
  next: string | null
}

export interface EventFilters {
  before?: string
  actor?: string
  organization?: string
  action?: string
  from?: string
  to?: string
  limit?: number
}
