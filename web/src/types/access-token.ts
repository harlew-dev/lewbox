// An account's own token for the Lewbox API (lbp_…). It acts as the
// account, with the account's rights.
export interface AccessToken {
  id: string
  user_id: string
  name: string
  prefix: string
  last4: string
  status: 'active' | 'expired' | 'revoked'
  created_at: string
  last_used_at?: string
  expires_at?: string
  revoked_at?: string
}

export interface NewAccessToken extends AccessToken {
  token: string
}

export interface AccessTokenList {
  data: AccessToken[]
}
