export type DocsBook = 'consumer' | 'platform'

export interface DocsItem {
  title: string
  path: string
  description?: string
  external?: string
}

export interface DocsGroup {
  title: string
  items: DocsItem[]
}

export interface DocsNav {
  groups: DocsGroup[]
}

export interface PageLink {
  title: string
  path: string
}

export interface DocsPage {
  path: string
  title: string
  description?: string
  markdown: string
  prev?: PageLink
  next?: PageLink
}
