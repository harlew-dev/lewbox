import type { Snapshot, SnapshotList, SnapshotListQuery } from '../../types/snapshot'
import { query, ref, request, toPage, type PageBody } from './client'

type SnapshotBody = PageBody<Snapshot> & {
  partial_nodes?: string[]
  summary?: SnapshotList['summary']
}

// Without a region, every subscribed region.
export async function listSnapshots(org: string, filter: SnapshotListQuery = {}): Promise<SnapshotList> {
  const body = await request<SnapshotBody>(`/v0/snapshots${query(filter)}`, { org })
  return {
    ...toPage(body),
    partial_nodes: body.partial_nodes ?? [],
    summary: body.summary,
  }
}

export function deleteSnapshot(org: string, snapshot: Pick<Snapshot, 'name' | 'region'>): Promise<Snapshot> {
  return request<Snapshot>(`/v0/snapshots/${ref(snapshot.name)}${query({ region: snapshot.region })}`, {
    method: 'DELETE',
    org,
  })
}
