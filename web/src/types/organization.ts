import type { Page } from '@harlew-dev/ui'
import type { ListParams } from './list'
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
  // Instance admins only: started sandboxes and the 30-day vCPU hours.
  running?: number
  vcpu_hours_30d?: number
  members: number
  role?: Role
  created_at: string
}

export interface OrganizationUpdate {
  name?: string
  status?: OrgStatus
}

export type OrganizationList = Page<Organization>

export interface OrganizationListQuery extends ListParams {
  status?: OrgStatus[]
  region?: string[]
  plan?: string[]
  // One account, by ID or email.
  user?: string
}

export interface Member {
  id: string
  organization_id: string
  user_id: string
  email: string
  role: Role
  created_at: string
}

export type MemberList = Page<Member>

export interface MemberListQuery extends ListParams {
  role?: Role[]
}
