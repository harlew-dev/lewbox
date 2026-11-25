import type { Snapshot, SnapshotList } from '../../types/snapshot'
import { query, ref, request } from './client'

// Without a region, every subscribed region.
export function listSnapshots(org: string, region?: string): Promise<SnapshotList> {
  return request<SnapshotList>(`/v0/snapshots${query({ region })}`, { org })
}

export function deleteSnapshot(org: string, snapshot: Pick<Snapshot, 'name' | 'region'>): Promise<Snapshot> {
  return request<Snapshot>(`/v0/snapshots/${ref(snapshot.name)}${query({ region: snapshot.region })}`, {
    method: 'DELETE',
    org,
  })
}
