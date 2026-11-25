import type { Page } from '@harlew-dev/ui'
import type { ListParams } from './list'
import type { PlanRef } from './plan'

export type SetupLinkStatus = 'unused' | 'claimed' | 'used' | 'expired'

// A single-use link that lets someone register, create a workspace and get a
// plan in a region. `token` is only present in the create response.
export interface SetupLink {
  id: string
  plan: PlanRef
  region: { slug: string; name: string }
  note: string
  status: SetupLinkStatus
  claimed_by: string | null
  workspace: { slug: string; name: string } | null
  created_at: string
  expires_at: string
  used_at: string | null
  token?: string
}

export interface SetupLinkInput {
  plan: string
  region: string
  // 1..720; 168 when omitted.
  expires_in_hours?: number
  note?: string
}

export type SetupLinkList = Page<SetupLink>

export interface SetupLinkListQuery extends ListParams {
  status?: SetupLinkStatus[]
  plan?: string[]
  region?: string[]
}

// What the public setup page shows: names only, never slugs.
export interface SetupLinkInfo {
  instance_name: string
  plan: { name: string }
  region: { name: string }
  expires_at: string
}
