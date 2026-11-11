import { Badge, type BadgeColor } from '@harlew-dev/ui'
import type * as api from '../../lib/api'

export const roleOptions: { value: api.Role; label: string; description: string }[] = [
  { value: 'owner', label: 'Owner', description: 'Everything, including making and removing owners.' },
  { value: 'admin', label: 'Admin', description: 'Manages members (except owners) and API keys.' },
  { value: 'member', label: 'Member', description: 'Uses the organization; can see its members.' },
  { value: 'viewer', label: 'Viewer', description: 'Read-only.' },
]

const roleColors: Record<api.Role, BadgeColor> = {
  owner: 'purple',
  admin: 'green',
  member: 'zinc',
  viewer: 'zinc',
}

export function RoleBadge({ role }: { role: api.Role }) {
  return <Badge color={roleColors[role]}>{role.charAt(0).toUpperCase() + role.slice(1)}</Badge>
}

const orgStatuses: Record<api.OrgStatus, { label: string; color: BadgeColor }> = {
  active: { label: 'Active', color: 'lime' },
  pending: { label: 'Pending', color: 'amber' },
  suspended: { label: 'Suspended', color: 'red' },
}

export function OrgStatusBadge({ status, reason }: { status: api.OrgStatus; reason?: string }) {
  const { label, color } = orgStatuses[status] ?? { label: status, color: 'zinc' }
  return (
    <Badge color={color} title={reason === 'credits' ? 'Out of credits' : undefined}>
      {label}
      {status === 'suspended' && reason === 'credits' ? ' · credits' : ''}
    </Badge>
  )
}
