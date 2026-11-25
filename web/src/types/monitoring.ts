export interface UptimeDay {
  date: string
  uptime: number | null
  down_minutes?: number
}

export interface ClusterHealth {
  cluster: { uptime: number | null; days: UptimeDay[] }
  nodes: { name: string; uptime: number | null; days: UptimeDay[] }[]
}

export type MetricsRange = '24h' | '7d' | '30d'

export interface NodeMetric {
  start: string
  sandboxes: number
  cpu_cores: number
  cpu_reserved: number
  memory_total_mb: number
  memory_used_mb: number
  disk_free_gb: number
  alive: number
}

export interface ClusterMetrics {
  range: MetricsRange
  bucket_seconds: number
  nodes: { name: string; data: NodeMetric[] }[]
}
