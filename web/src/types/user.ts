export interface User {
  id: string
  email: string
  instance_admin: boolean
  disabled: boolean
  created_at: string
}

export interface UserList {
  data: User[]
}
