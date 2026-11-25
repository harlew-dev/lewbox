import type { Member, MemberList, Role } from '../../types/organization'
import { ref, request } from './client'

export function listMembers(org: string): Promise<MemberList> {
  return request<MemberList>('/v0/members', { org })
}

export function addMember(org: string, input: { email: string; role: Role }): Promise<Member> {
  return request<Member>('/v0/members', { method: 'POST', body: input, org })
}

export function setMemberRole(org: string, memberRef: string, role: Role): Promise<Member> {
  return request<Member>(`/v0/members/${ref(memberRef)}`, {
    method: 'PATCH',
    body: { role },
    org,
  })
}

export function removeMember(org: string, memberRef: string): Promise<Member> {
  return request<Member>(`/v0/members/${ref(memberRef)}`, { method: 'DELETE', org })
}
