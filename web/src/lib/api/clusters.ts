import type {
  Cluster,
  ClusterDetail,
  ClusterList,
  ClusterUpdate,
  ClusterUpgrade,
  ClusterUpgradePlan,
  ClusterUpgradePlanInput,
  ClusterUpgradeStartInput,
  JoinToken,
} from '../../types/cluster'
import type { ClusterHealth, ClusterMetrics, MetricsRange } from '../../types/monitoring'
import { query, ref, request } from './client'

export function listClusters(): Promise<ClusterList> {
  return request<ClusterList>('/v0/clusters')
}

export function getCluster(clusterRef: string): Promise<ClusterDetail> {
  return request<ClusterDetail>(`/v0/clusters/${ref(clusterRef)}`)
}

export function checkCluster(clusterRef: string): Promise<ClusterDetail> {
  return request<ClusterDetail>(`/v0/clusters/${ref(clusterRef)}/check`, {
    method: 'POST',
  })
}

export function removeCluster(clusterRef: string): Promise<Cluster> {
  return request<Cluster>(`/v0/clusters/${ref(clusterRef)}`, {
    method: 'DELETE',
  })
}

export function addCluster(input: {
  slug: string
  name: string
  admin_url: string
  endpoint_url: string
  runtime: string
  admin_token: string
  docker_timeout?: string
  max_sandbox_cpu?: number
  max_sandbox_memory_mb?: number
  max_sandbox_disk_gb?: number
  idle_stop_minutes?: number
  destroy_if_idle_for?: string
  max_age?: string
  firecracker_vm_slots?: number
}): Promise<ClusterDetail> {
  return request<ClusterDetail>('/v0/clusters', { method: 'POST', body: input })
}

export function createJoinToken(
  clusterRef: string,
  input: { role: string; ttl: string },
): Promise<JoinToken> {
  return request<JoinToken>(`/v0/clusters/${ref(clusterRef)}/join-tokens`, {
    method: 'POST',
    body: input,
  })
}

export function updateCluster(clusterRef: string, input: ClusterUpdate): Promise<Cluster> {
  return request<Cluster>(`/v0/clusters/${ref(clusterRef)}`, { method: 'PATCH', body: input })
}

export function clusterHealth(clusterRef: string, days = 30): Promise<ClusterHealth> {
  return request<ClusterHealth>(`/v0/clusters/${ref(clusterRef)}/health${query({ days })}`)
}

export function clusterMetrics(clusterRef: string, range: MetricsRange): Promise<ClusterMetrics> {
  return request<ClusterMetrics>(`/v0/clusters/${ref(clusterRef)}/metrics${query({ range })}`)
}

// The cluster's current rollout. A cluster that never had one answers 404.
export function getClusterUpgrade(clusterRef: string): Promise<ClusterUpgrade> {
  return request<ClusterUpgrade>(`/v0/clusters/${ref(clusterRef)}/upgrade`)
}

export function planClusterUpgrade(
  clusterRef: string,
  input: ClusterUpgradePlanInput = {},
): Promise<ClusterUpgradePlan> {
  return request<ClusterUpgradePlan>(`/v0/clusters/${ref(clusterRef)}/upgrade/plan${query(input)}`)
}

export function startClusterUpgrade(
  clusterRef: string,
  input: ClusterUpgradeStartInput,
): Promise<ClusterUpgrade> {
  return request<ClusterUpgrade>(`/v0/clusters/${ref(clusterRef)}/upgrade`, {
    method: 'POST',
    body: input,
  })
}

export function stopClusterUpgrade(clusterRef: string): Promise<ClusterUpgrade> {
  return request<ClusterUpgrade>(`/v0/clusters/${ref(clusterRef)}/upgrade/stop`, { method: 'POST' })
}

export function resumeClusterUpgrade(clusterRef: string): Promise<ClusterUpgrade> {
  return request<ClusterUpgrade>(`/v0/clusters/${ref(clusterRef)}/upgrade/resume`, { method: 'POST' })
}

export function rollbackClusterUpgrade(clusterRef: string): Promise<ClusterUpgrade> {
  return request<ClusterUpgrade>(`/v0/clusters/${ref(clusterRef)}/upgrade/rollback`, { method: 'POST' })
}

export function finalizeClusterUpgrade(clusterRef: string): Promise<ClusterUpgrade> {
  return request<ClusterUpgrade>(`/v0/clusters/${ref(clusterRef)}/upgrade/finalize`, { method: 'POST' })
}
