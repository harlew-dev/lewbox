export interface AdminKey {
  id: string
  organization_id: string
  name: string
  prefix: string
  last4: string
  status: 'active' | 'expired' | 'revoked'
  created_by?: string
  created_at: string
  last_used_at?: string
  expires_at?: string
  revoked_at?: string
}

export interface NewAdminKey extends AdminKey {
  token: string
}

export interface AdminKeyList {
  data: AdminKey[]
}
