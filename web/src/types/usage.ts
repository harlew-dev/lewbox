export interface UsageAmounts {
  sandbox_hours: number
  vcpu_hours: number
  memory_gib_hours: number
}

export interface UsageBucket extends UsageAmounts {
  start: string
  peak_sandboxes: number
  regions: Record<string, { vcpu_hours: number }>
}

export interface Usage {
  from: string
  to: string
  granularity: 'hour' | 'day'
  totals: UsageAmounts
  data: UsageBucket[]
}
