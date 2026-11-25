import type { Page } from '@harlew-dev/ui'
import type { ListParams } from './list'
import type { KeyStatus } from './sandbox-key'

// An account's own token for the Lewbox API (lbp_…). It acts as the
// account, with the account's rights.
export interface AccessToken {
  id: string
  user_id: string
  name: string
  prefix: string
  last4: string
  status: KeyStatus
  created_at: string
  last_used_at?: string
  expires_at?: string
  revoked_at?: string
}

export interface NewAccessToken extends AccessToken {
  token: string
}

export type AccessTokenList = Page<AccessToken>

export interface AccessTokenListQuery extends ListParams {
  status?: KeyStatus[]
}
