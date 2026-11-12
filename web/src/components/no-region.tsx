import { EmptyState } from '@harlew-dev/ui'

// A workspace can use a region only once the operator gives it a plan there.
export const noRegionText = 'Your workspace doesn’t have a region yet. Your provider will enable one.'

// The portal's empty state for a workspace with no region.
export function NoRegion() {
  return <EmptyState title="No region yet" description={noRegionText} />
}
