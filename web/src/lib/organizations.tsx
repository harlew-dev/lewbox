import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import type * as api from './api'
import { useAuth } from './auth'

// The organizations the signed-in account belongs to (from /v0/me) and the
// one the portal shows: the last one picked on this browser, otherwise the
// first.
interface OrganizationsValue {
  organizations: api.MeOrganization[]
  current: api.MeOrganization | null
  select: (slug: string) => void
}

const OrganizationsContext = createContext<OrganizationsValue>({
  organizations: [],
  current: null,
  select: () => {},
})

const storageKey = 'lewbox:org'

function remembered(): string | null {
  try {
    return localStorage.getItem(storageKey)
  } catch {
    return null
  }
}

function remember(slug: string) {
  try {
    localStorage.setItem(storageKey, slug)
  } catch {
    // Not remembered; the switcher still works for this visit.
  }
}

export function OrganizationsProvider({ children }: { children: ReactNode }) {
  const { organizations } = useAuth()
  const [picked, setPicked] = useState(remembered)

  const select = useCallback((slug: string) => {
    remember(slug)
    setPicked(slug)
  }, [])

  const current = useMemo(
    () => organizations.find((org) => org.slug === picked) ?? organizations[0] ?? null,
    [organizations, picked],
  )
  const value = useMemo(() => ({ organizations, current, select }), [organizations, current, select])
  return <OrganizationsContext.Provider value={value}>{children}</OrganizationsContext.Provider>
}

export function useOrganizations(): OrganizationsValue {
  return useContext(OrganizationsContext)
}

export type Permissions = ReturnType<typeof orgPermissions>

// What the caller may do in an organization, from its role there.
export function orgPermissions(org: { role?: api.Role; status: api.OrgStatus }, instanceAdmin: boolean) {
  const manager = org.role === 'owner' || org.role === 'admin'
  const writer = manager || org.role === 'member'
  const active = org.status === 'active'
  return {
    // See and create management API keys.
    keys: instanceAdmin || manager,
    // Create sandbox API keys (only while the organization is active).
    use: instanceAdmin || (writer && active),
    // Stop and delete sandboxes.
    sandboxes: instanceAdmin || writer,
    // Change the organization, its members and its keys.
    manage: instanceAdmin || (manager && active),
    // Hand out or take away the owner role.
    owner: instanceAdmin || org.role === 'owner',
  }
}

// Up to two initials for an organization's avatar.
export function orgInitials(name: string): string {
  return (
    name
      .split(/[\s._-]+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((part) => part[0])
      .join('')
      .toUpperCase() || '?'
  )
}
