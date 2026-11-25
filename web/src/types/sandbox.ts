export type SandboxStatus = 'creating' | 'started' | 'stopped' | 'destroyed' | 'error'

export interface Sandbox {
  id: string
  region: string
  organization?: { slug: string; name: string }
  name: string
  image: string
  status: SandboxStatus | string
  status_reason: string
  runtime: string
  cpu: number
  memory_mb: number
  disk_gb: number
  node?: string
  created_at: string
  last_seen_at: string
}

export interface SandboxFilter {
  region?: string
  status?: string
  org?: string
}
