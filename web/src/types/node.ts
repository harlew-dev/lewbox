import type { Page } from '@harlew-dev/ui'
import type { ListParams } from './list'

export interface Node {
  id: string
  name: string
  url: string
  created_at: string
  cluster?: string
  role?: string
  version?: string
  alive?: boolean
  last_seen_at?: string
  // What the node last applied, and whether it is behind its cluster's
  // profiles. Null for a node in no cluster.
  settings?: NodeSettingsState | null
}

export interface NodeSettingsState {
  applied_at: string | null
  outdated: boolean
  reasons: string[]
}

export type NodeList = Page<Node>

export interface NodeListQuery extends ListParams {
  cluster?: string[]
  role?: string[]
  alive?: boolean
}
