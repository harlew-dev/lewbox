import type { Node } from './node'

// How long nodes keep their local copy of an archived sandbox before the
// cleanup timer removes it. The bucket always holds the archive.
export type ArchiveRetention = 'instant' | '1h' | '1d' | '7d' | 'never'

// How archive snapshots are named: archive-<id> or archive-<name>-<id>.
export type ArchiveSnapshotName = 'id' | 'name'

// The AerolVM tuning every node of a cluster applies. A null value is off,
// or AerolVM's own default; docker_timeout always has a value.
export interface AdvancedSettings {
  docker_timeout: string
  max_sandbox_cpu: number | null
  max_sandbox_memory_mb: number | null
  max_sandbox_disk_gb: number | null
  idle_stop_minutes: number | null
  destroy_if_idle_for: string
  max_age: string
  firecracker_vm_slots: number | null
}

export interface Cluster extends AdvancedSettings {
  id: string
  slug: string
  name: string
  admin_url: string
  endpoint_url: string
  runtime: string
  domain: string
  dns_profile: string | null
  dns_records: 'auto' | 'off'
  storage_profile: string | null
  stop_policy: 'keep' | 'archive'
  archive_local_retention: ArchiveRetention
  archive_snapshot_name: ArchiveSnapshotName
  admin_token_last4: string
  status: string
  status_detail?: string
  version?: string
  nodes: number
  checked_at?: string
  created_at: string
}

export interface Check {
  name: string
  ok: boolean
  fatal?: boolean
  detail?: string
}

export interface ClusterDetail extends Cluster {
  checks: Check[]
  node_list: Node[]
}

export interface JoinToken {
  cluster_id: string
  cluster: string
  runtime: string
  version: string
  role: string
  token_role: string
  token: string
  ca_cert_hash: string
  join_urls: string[]
  expires_at: string
  command: string
}

export interface ClusterList {
  data: Cluster[]
}

// Only the fields that are set change. dns_profile and storage_profile take
// 'none' to clear; dns_records is 'auto' or 'off'.
export interface ClusterUpdate {
  domain?: string
  dns_profile?: string
  dns_records?: 'auto' | 'off'
  storage_profile?: string
  stop_policy?: 'keep' | 'archive'
  archive_local_retention?: ArchiveRetention
  archive_snapshot_name?: ArchiveSnapshotName
  // Advanced settings: null clears one; an absent field is unchanged.
  docker_timeout?: string | null
  max_sandbox_cpu?: number | null
  max_sandbox_memory_mb?: number | null
  max_sandbox_disk_gb?: number | null
  idle_stop_minutes?: number | null
  destroy_if_idle_for?: string | null
  max_age?: string | null
  firecracker_vm_slots?: number | null
}
