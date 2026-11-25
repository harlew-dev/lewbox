import type {
  SetupLink,
  SetupLinkInfo,
  SetupLinkInput,
  SetupLinkList,
  SetupLinkListQuery,
} from '../../types/setup-link'
import { query, ref, request, toPage, type PageBody } from './client'

export async function listSetupLinks(filter: SetupLinkListQuery = {}): Promise<SetupLinkList> {
  return toPage(await request<PageBody<SetupLink>>(`/v0/setup-links${query(filter)}`))
}

export function createSetupLink(input: SetupLinkInput): Promise<SetupLink> {
  return request<SetupLink>('/v0/setup-links', { method: 'POST', body: input })
}

export function revokeSetupLink(id: string): Promise<SetupLink> {
  return request<SetupLink>(`/v0/setup-links/${ref(id)}`, { method: 'DELETE' })
}

// Public: the plan and region names behind a setup token. The token travels
// in the body, never in the URL.
export function setupLinkInfo(token: string): Promise<SetupLinkInfo> {
  return request<SetupLinkInfo>('/v0/setup-links/check', {
    method: 'POST',
    body: { token },
    authRedirect: false,
  })
}
