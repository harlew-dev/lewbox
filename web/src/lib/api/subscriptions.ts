import type { Subscription, SubscriptionInput } from '../../types/region'
import { query, ref, request } from './client'

export function listSubscriptions(org: string): Promise<{ data: Subscription[] }> {
  return request<{ data: Subscription[] }>('/v0/subscriptions', { org })
}

export function setSubscription(org: string, region: string, input: SubscriptionInput): Promise<Subscription> {
  return request<Subscription>(`/v0/subscriptions/${ref(region)}`, { method: 'PATCH', body: input, org })
}

// force removes it even when the region's keys can't be revoked.
export function deleteSubscription(org: string, region: string, force = false): Promise<Subscription> {
  return request<Subscription>(`/v0/subscriptions/${ref(region)}${query({ force: force ? 'true' : undefined })}`, {
    method: 'DELETE',
    org,
  })
}
