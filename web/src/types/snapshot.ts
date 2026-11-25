import type { Page } from '@harlew-dev/ui'
import type { ListParams } from './list'

export type SnapshotKind = 'snapshot' | 'archive'

// AerolVM's push states, the one vocabulary for both snapshots and archives.
// An empty status is a cluster with no registry, which never pushes.
export type SnapshotStatus = 'pending' | 'pushing' | 'active' | 'error' | ''

export interface Snapshot {
  // The archive's UUIDv7; empty for a plain snapshot.
  id: string
  kind: SnapshotKind
  // A plain snapshot's name, or an archive's snapshot name.
  name: string
  region: string
  // Only for instance admins: the owning workspace and, for a plain
  // snapshot, the node that holds it. An archive lives in the bucket.
  organization?: { slug: string; name: string }
  node?: string
  // The sandbox an archive was taken from.
  sandbox_name: string
  source_sandbox_id: string
  image: string
  runtime: string
  created_at: string
  archived_at: string | null
  cpu: number
  memory_mb: number
  disk_gb: number
  tags: Record<string, string>
  status: SnapshotStatus
  error: string
  registry_ref: string
  last_restored_sandbox_id: string
  last_restored_at: string | null
}

// Totals for the whole filtered set, from ?summary=include.
export interface SnapshotSummary {
  count: number
  disk_gb: number
  memory_mb: number
  failed: number
  workspaces: number
  by_node: {
    region: string
    node: string | null
    kind: SnapshotKind
    count: number
    disk_gb: number
  }[]
  by_workspace: { slug: string; name: string; count: number; disk_gb: number }[]
}

export type SnapshotList = Page<Snapshot> & {
  // Nodes that didn't answer, so the list may be missing some.
  partial_nodes: string[]
  summary?: SnapshotSummary
}

export interface SnapshotListQuery extends ListParams {
  kind?: SnapshotKind[]
  region?: string[]
  // Instance admins only.
  organization?: string[]
  status?: SnapshotStatus[]
  summary?: 'include'
}

export interface RestoredArchive {
  archive: Snapshot
  sandbox: { id: string; name: string; status: string; region: string }
}
