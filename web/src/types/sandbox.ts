import type { Page } from '@harlew-dev/ui'
import type { ListParams } from './list'

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

export type SandboxList = Page<Sandbox>

export interface SandboxListQuery extends ListParams {
  status?: string[]
  region?: string[]
  // Instance admins only.
  organization?: string[]
  // The workspace whose sandboxes these are; sent as the header and ?org=.
  org?: string
}
