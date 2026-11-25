import type { OrgStatus, Organization, OrganizationList, OrganizationUpdate } from '../../types/organization'
import { query, ref, request } from './client'

export function listOrganizations(filter: { user?: string; status?: OrgStatus } = {}): Promise<OrganizationList> {
  return request<OrganizationList>(`/v0/organizations${query(filter)}`)
}

export function getOrganization(orgRef: string): Promise<Organization> {
  return request<Organization>(`/v0/organizations/${ref(orgRef)}`)
}

export function createOrganization(input: {
  name: string
  slug?: string
  owner?: string
  setup_token?: string
}): Promise<Organization> {
  return request<Organization>('/v0/organizations', { method: 'POST', body: input })
}

export function updateOrganization(orgRef: string, input: OrganizationUpdate): Promise<Organization> {
  return request<Organization>(`/v0/organizations/${ref(orgRef)}`, {
    method: 'PATCH',
    body: input,
  })
}
