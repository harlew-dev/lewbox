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

// One node's progress in a rolling upgrade.
export interface ClusterUpgradeNode {
  node: string
  role: string
  from_version?: string
  to_version?: string
  state: string
  order: number
  batch?: number
  error?: string
  migrated: boolean
  updated_at: string
}

// The cluster's current AerolVM rollout.
export interface ClusterUpgrade {
  cluster: string
  cluster_id: string
  id: string
  phase: string
  direction: string
  target_version: string
  previous_version?: string
  source: string
  hold: boolean
  batch: string
  pause_after?: number
  allow_downtime: boolean
  error?: string
  created_at: string
  updated_at: string
  nodes: ClusterUpgradeNode[]
}

export interface ClusterUpgradeSkew {
  upgrade_from_min?: string
  below: string[]
}

export interface ClusterUpgradePlanNode {
  node: string
  role: string
  version: string
  target_version: string
  alive: boolean
  voter: boolean
  leader: boolean
  order?: number
  batch?: number
  action: 'upgrade' | 'skip_target' | 'skip_down'
}

// What an upgrade would do, and nothing more.
export interface ClusterUpgradePlan {
  cluster: string
  cluster_id: string
  target_version: string
  tested: boolean
  source: string
  allowed: boolean
  refusals: string[]
  warnings: string[]
  needs_allow_downtime: boolean
  skew: ClusterUpgradeSkew
  one_way_migrations: number[]
  rollback_after_finalize: boolean
  batch: string
  batch_size: number
  pause_after?: number
  nodes: ClusterUpgradePlanNode[]
  metadata: Record<string, unknown>
}

export interface ClusterUpgradePlanInput {
  version?: string
  batch?: string
  pause_after?: number
}

export interface ClusterUpgradeStartInput {
  version?: string
  batch?: string
  pause_after?: number
  hold?: boolean
  allow_downtime?: boolean
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
