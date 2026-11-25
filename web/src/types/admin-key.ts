import type { Page } from '@harlew-dev/ui'
import type { ListParams } from './list'
import type { KeyStatus } from './sandbox-key'

export interface AdminKey {
  id: string
  organization_id: string
  name: string
  prefix: string
  last4: string
  status: KeyStatus
  created_by?: string
  created_at: string
  last_used_at?: string
  expires_at?: string
  revoked_at?: string
}

export interface NewAdminKey extends AdminKey {
  token: string
}

export type AdminKeyList = Page<AdminKey>

export interface AdminKeyListQuery extends ListParams {
  status?: KeyStatus[]
}
