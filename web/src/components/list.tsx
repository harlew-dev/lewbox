import { Button, EmptyState, ListSkeleton, TableHeader } from '@harlew-dev/ui'
import type { ReactNode } from 'react'
import type { ListSpec, ListState } from '../lib/list-query'
import { sortHeader } from '../lib/list-query'

// The two empty states a paged list must tell apart: nothing exists yet, or
// records exist but none match the search and filters.
export function NoRecords({
  title,
  description,
  action,
}: {
  title: string
  description: string
  action?: ReactNode
}) {
  return <EmptyState title={title} description={description} action={action} />
}

export function NoMatches({ label, onClear }: { label: string; onClear: () => void }) {
  return (
    <EmptyState
      title={`No ${label} match`}
      description="Try another search, or clear the filters."
      action={
        <Button plain onClick={onClear}>
          Clear filters
        </Button>
      }
    />
  )
}

// A sortable header: clicking the current column reverses it; another starts
// in the list's natural direction for that column.
export function SortHeader({
  state,
  spec,
  column,
  base,
  className,
  children,
}: {
  state: ListState
  spec: ListSpec
  column: string
  base?: URLSearchParams
  className?: string
  children: ReactNode
}) {
  const { href, sort } = sortHeader(state, spec, column, base)
  return (
    <TableHeader href={href} sort={sort} className={className}>
      {children}
    </TableHeader>
  )
}

// The table's placeholder while its first answer is on its way.
export function LoadingList({ label }: { label: string }) {
  return <ListSkeleton label={label} rows={4} />
}
