import type { Page } from '@harlew-dev/ui'

// What every paged list accepts in its query string. Defaults (page 1, 20
// rows, no search, the list's default sort) are left out of the URL.
export interface ListParams {
  page?: number
  per_page?: number
  q?: string
  sort?: string
}

export type ListPage<T> = Page<T>
