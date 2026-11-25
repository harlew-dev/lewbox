import type { Settings, SettingsUpdate } from '../../types/settings'
import { request } from './client'

export function getSettings(): Promise<Settings> {
  return request<Settings>('/v0/settings')
}

export function updateSettings(input: SettingsUpdate): Promise<Settings> {
  return request<Settings>('/v0/settings', { method: 'PATCH', body: input })
}
