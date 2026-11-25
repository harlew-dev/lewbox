// null is unlimited.
export interface Limits {
  sandboxes: number | null
  vcpus: number | null
  memory_mb: number | null
}

export interface Plan {
  id: string
  slug: string
  name: string
  max_sandboxes: number | null
  max_vcpus: number | null
  max_memory_mb: number | null
  // Subscriptions on this plan, across every workspace and region.
  subscriptions: number
  default: boolean
  created_at: string
}

export interface PlanInput {
  slug?: string
  name?: string
  max_sandboxes?: number | null
  max_vcpus?: number | null
  max_memory_mb?: number | null
}

export interface PlanRef {
  slug: string
  name: string
}
