import type { Page } from '@harlew-dev/ui'
import type { ListParams } from './list'

export type UserStatus = 'active' | 'disabled'
export type UserRole = 'admin' | 'user'

export interface User {
  id: string
  email: string
  instance_admin: boolean
  disabled: boolean
  created_at: string
}

export type UserList = Page<User>

export interface UserListQuery extends ListParams {
  status?: UserStatus[]
  role?: UserRole[]
}
