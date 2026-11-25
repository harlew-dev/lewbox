import type { User, UserList } from '../../types/user'
import { ref, request } from './client'

export function listUsers(): Promise<UserList> {
  return request<UserList>('/v0/users')
}

export function createUser(input: {
  email: string
  password: string
  instance_admin: boolean
}): Promise<User> {
  return request<User>('/v0/users', { method: 'POST', body: input })
}

export function setUserDisabled(userRef: string, disabled: boolean): Promise<User> {
  return request<User>(`/v0/users/${ref(userRef)}/suspension`, {
    method: disabled ? 'POST' : 'DELETE',
  })
}

export function removeUser(userRef: string): Promise<User> {
  return request<User>(`/v0/users/${ref(userRef)}`, { method: 'DELETE' })
}

// Changing a password needs the caller's own current password as fresh proof.
export function setPassword(
  userRef: string,
  input: { password: string; current_password: string },
): Promise<User> {
  return request<User>(`/v0/users/${ref(userRef)}/password`, {
    method: 'PATCH',
    body: input,
  })
}

export function getUser(userRef: string): Promise<User> {
  return request<User>(`/v0/users/${ref(userRef)}`)
}
