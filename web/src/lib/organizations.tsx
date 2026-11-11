import { createContext, useCallback, useContext, useEffect, useMemo, type ReactNode } from 'react'
import { matchPath, useLocation } from 'react-router'
import * as api from './api'
import { useAuth } from './auth'
import { useLoad } from './use-load'

// The organizations the signed-in account can see, and the current one: the
// organization page being shown, otherwise the last one opened on this
// browser, otherwise the first.
interface OrganizationsValue {
  organizations: api.Organization[]
  loading: boolean
  current: api.Organization | null
  reload: () => void
}

const OrganizationsContext = createContext<OrganizationsValue>({
  organizations: [],
  loading: false,
  current: null,
  reload: () => {},
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

// The organization slug in the current URL, if any.
export function useOrgSlugFromURL(): string | null {
  const location = useLocation()
  return matchPath('/organizations/:org/*', location.pathname)?.params.org ?? null
}

export function OrganizationsProvider({ children }: { children: ReactNode }) {
  const { user } = useAuth()
  const load = useLoad(() => api.listOrganizations(), [user?.id])
  const fromURL = useOrgSlugFromURL()
  const organizations = useMemo(() => load.data?.data ?? [], [load.data])

  useEffect(() => {
    if (fromURL) remember(fromURL)
  }, [fromURL])

  const current = useMemo(() => {
    const wanted = fromURL ?? remembered()
    return organizations.find((org) => org.slug === wanted) ?? organizations[0] ?? null
  }, [organizations, fromURL])

  const reload = useCallback(() => load.reload(), [load])
  const value = useMemo(
    () => ({ organizations, loading: load.loading, current, reload }),
    [organizations, load.loading, current, reload],
  )
  return <OrganizationsContext.Provider value={value}>{children}</OrganizationsContext.Provider>
}

export function useOrganizations(): OrganizationsValue {
  return useContext(OrganizationsContext)
}

// What the caller may do in an organization, from its role there.
export function orgPermissions(org: api.Organization, instanceAdmin: boolean) {
  const manager = org.role === 'owner' || org.role === 'admin'
  return {
    // See and create admin keys.
    keys: instanceAdmin || manager,
    // Change the organization, its members and its keys.
    manage: instanceAdmin || (manager && org.status === 'active'),
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
