// A saved Cloudflare token and the zone it reaches. The token is never returned.
export interface DNSProfile {
  id: string
  name: string
  provider: string
  zone: string
  zone_id: string
  token_last4: string
  acme_email: string
  version: number
  clusters: number
  tested_at: string | null
  test_error: string
  created_at: string
  updated_at: string
}

export interface DNSProfileInput {
  name?: string
  zone?: string
  // Omitted on an update to keep the current token.
  api_token?: string
  acme_email?: string
}
