import type { DNSProfile, DNSProfileInput } from '../../types/dns-profile'
import { ref, request } from './client'

export function listDNSProfiles(): Promise<{ data: DNSProfile[] }> {
  return request<{ data: DNSProfile[] }>('/v0/dns-profiles')
}

export function createDNSProfile(input: DNSProfileInput & { name: string; zone: string; api_token: string }): Promise<DNSProfile> {
  return request<DNSProfile>('/v0/dns-profiles', { method: 'POST', body: { provider: 'cloudflare', ...input } })
}

export function updateDNSProfile(profileRef: string, input: DNSProfileInput): Promise<DNSProfile> {
  return request<DNSProfile>(`/v0/dns-profiles/${ref(profileRef)}`, { method: 'PATCH', body: input })
}

export function deleteDNSProfile(profileRef: string): Promise<DNSProfile> {
  return request<DNSProfile>(`/v0/dns-profiles/${ref(profileRef)}`, { method: 'DELETE' })
}

export function testDNSProfile(profileRef: string): Promise<DNSProfile> {
  return request<DNSProfile>(`/v0/dns-profiles/${ref(profileRef)}/check`, { method: 'POST' })
}
