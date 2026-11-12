import * as api from './api'
import { useLoad } from './use-load'

export interface Regions {
  list: api.Region[]
  loaded: boolean
  // A region's name for its slug. A slug not in the list (a region since
  // removed) shows as itself.
  name: (slug: string) => string
}

// The regions from /v0/regions: a customer gets the ones `org` has a plan
// in, an instance admin every ready one. People read a region's name,
// never its slug.
export function useRegions(org?: string): Regions {
  const regions = useLoad(() => api.listRegions(org), [org])
  const list = regions.data?.data ?? []
  const names = new Map(list.map((region) => [region.slug, region.name]))
  return { list, loaded: regions.data !== null, name: (slug) => names.get(slug) ?? slug }
}
