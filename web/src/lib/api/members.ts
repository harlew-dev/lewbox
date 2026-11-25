import type {
  Member,
  MemberList,
  MemberListQuery,
  Role,
} from '../../types/organization'
import { query, ref, request, toPage, type PageBody } from './client'

export async function listMembers(org: string, filter: MemberListQuery = {}): Promise<MemberList> {
  return toPage(await request<PageBody<Member>>(`/v0/members${query(filter)}`, { org }))
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
