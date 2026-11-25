import type { Node } from './node'

// How long nodes keep their local copy of an archived sandbox before the
// cleanup timer removes it. The bucket always holds the archive.
export type ArchiveRetention = 'instant' | '1h' | '1d' | '7d' | 'never'

// How archive snapshots are named: archive-<id> or archive-<name>-<id>.
export type ArchiveSnapshotName = 'id' | 'name'

export interface Cluster {
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
}
