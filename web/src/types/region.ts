import type { Limits, PlanRef } from './plan'

// A cluster as customers see it. For a customer, plan and limits are
// their workspace's subscription there.
export interface Region {
  slug: string
  name: string
  endpoint_url: string
  runtime: string
  status?: string
  // 'keep' leaves a stopped sandbox on its node; 'archive' moves it to storage.
  stop_policy?: 'keep' | 'archive'
  plan?: PlanRef
  limits?: Limits
}

// A workspace's plan in one region. overrides is only sent to instance
// admins; a null override uses the plan's limit.
export interface Subscription {
  region: Region
  plan: PlanRef
  limits: Limits
  overrides?: Limits
  created_at: string
}

// An absent override is unchanged (null on create); null uses the plan's.
export interface SubscriptionInput {
  plan: string
  max_sandboxes?: number | null
  max_vcpus?: number | null
  max_memory_mb?: number | null
}

export interface LimitsUsed {
  sandboxes: number
  vcpus: number
  memory_mb: number
}

// One subscribed region: its plan's limits and what is in use now.
export interface RegionLimits {
  slug: string
  name: string
  plan: PlanRef
  limits: Limits
  used: LimitsUsed
  // Why the limits couldn't be pushed to the region, or "".
  error: string
}

export interface LimitsReport {
  regions: RegionLimits[]
}
