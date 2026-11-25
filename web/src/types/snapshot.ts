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

export interface SnapshotList {
  data: Snapshot[]
  // Nodes that didn't answer, so the list may be missing some.
  partial_nodes: string[]
}

export interface RestoredArchive {
  archive: Snapshot
  sandbox: { id: string; name: string; status: string; region: string }
}
