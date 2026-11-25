import type { UptimeDay } from './monitoring'

export interface Overview {
  customers: { total: number; pending: number; suspended: number }
  sandboxes: { running: number; stopped: number }
  capacity: {
    nodes_alive: number
    nodes: number
    cpu_cores: number
    cpu_reserved: number
    memory_total_mb: number
    memory_used_mb: number
  }
  clusters: { slug: string; name: string; status: string; uptime: number | null; days: UptimeDay[] }[]
  running: { start: string; sandboxes: number }[]
  top_customers: { slug: string; name: string; vcpu_hours: number }[]
}
