import { Badge } from '@harlew-dev/ui'
import type { BadgeColor } from '@harlew-dev/ui'

const statusColors: Record<string, BadgeColor> = {
  ready: 'lime',
  degraded: 'amber',
  unreachable: 'red',
}

export function StatusBadge({ status, detail }: { status: string; detail?: string }) {
  const label = status === '' ? 'unknown' : status
  return (
    <Badge color={statusColors[label] ?? 'zinc'} title={detail}>
      {label.charAt(0).toUpperCase() + label.slice(1)}
    </Badge>
  )
}

export function AliveBadge({ alive }: { alive?: boolean }) {
  if (alive === undefined) return <Badge color="zinc">Unknown</Badge>
  return <Badge color={alive ? 'lime' : 'red'}>{alive ? 'Alive' : 'Down'}</Badge>
}
