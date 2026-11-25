import type { OrgStatus, Role } from './organization'
import type { User } from './user'

export interface MeOrganization {
  id: string
  slug: string
  name: string
  role: Role
  status: OrgStatus
}

export interface Me {
  user?: User
  organizations?: MeOrganization[]
  via: string
  // Only for a browser session: sent back as X-CSRF-Token on writes.
  csrf_token?: string
}

export type SignupMode = 'open' | 'approval' | 'closed'

// Public: what the sign-in page and the portal need before anyone signs in.
export interface Instance {
  name: string
  signup: SignupMode
}

export interface SessionResponse {
  expires_at: string
  user: User
  csrf_token?: string
}
