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

export function OrgStatusBadge({ status }: { status: api.Organization['status'] }) {
  return status === 'active' ? <Badge color="lime">Active</Badge> : <Badge color="amber">Suspended</Badge>
}
