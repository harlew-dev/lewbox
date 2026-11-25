import type { Region } from './region'

export interface SandboxKey {
  id: string
  name: string
  region: Region
  token_hint: string
  created_by?: string
  created_at: string
  expires_at: string | null
  revoked_at: string | null
  expired: boolean
}

export interface NewSandboxKey extends SandboxKey {
  token: string
  env: { SB_API_URL: string; SB_API_TOKEN: string }
  // Only in a region that runs the E2B API (the docker runtime).
  e2b_env?: { E2B_API_URL: string; E2B_SANDBOX_URL: string; E2B_API_KEY: string }
}
