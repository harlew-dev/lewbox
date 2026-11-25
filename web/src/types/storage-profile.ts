// A bucket for snapshots and archives. The secret is never returned.
export interface StorageProfile {
  id: string
  name: string
  provider: string
  endpoint: string
  region: string
  bucket: string
  prefix: string
  path_style: boolean
  access_key_id: string
  secret_last4: string
  version: number
  clusters: number
  tested_at: string | null
  test_error: string
  created_at: string
  updated_at: string
}

export interface StorageProfileInput {
  name?: string
  endpoint?: string
  region?: string
  bucket?: string
  prefix?: string
  path_style?: boolean
  access_key_id?: string
  // Omitted on an update to keep the current secret.
  secret_access_key?: string
}
