import type { PlanRef } from './plan'

export type Role = 'owner' | 'admin' | 'member' | 'viewer'

export type OrgStatus = 'pending' | 'active' | 'suspended'

export interface Organization {
  id: string
  slug: string
  name: string
  status: OrgStatus
  status_reason?: string
  team?: string
  // The one region the workspace runs in, with its plan, or null.
  region: { slug: string; name: string; plan: PlanRef } | null
  // Instance admins only.
  vcpu_hours_30d?: number
  members: number
  role?: Role
  created_at: string
}

export interface OrganizationUpdate {
  name?: string
  status?: OrgStatus
}

export interface OrganizationList {
  data: Organization[]
}

export interface Member {
  id: string
  organization_id: string
  user_id: string
  email: string
  role: Role
  created_at: string
}

export interface MemberList {
  data: Member[]
}
