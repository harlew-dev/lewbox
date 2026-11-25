import { Badge, SidebarHeading, SidebarItem, SidebarLabel, SidebarSection } from '@harlew-dev/ui'
import {
  BuildingOffice2Icon,
  Cog6ToothIcon,
  CubeIcon,
  HomeIcon,
  RectangleStackIcon,
  CameraIcon,
  ClipboardDocumentListIcon,
  ServerStackIcon,
  UserCircleIcon,
  UsersIcon,
} from '@heroicons/react/20/solid'
import { createContext, useContext, type ReactNode } from 'react'
import * as api from '../lib/api'
import { useAuth } from '../lib/auth'
import { routes } from '../lib/routes'
import { useLoad } from '../lib/use-load'
import { AppShell, useIsActive } from './app-shell'

const PendingContext = createContext<() => void>(() => {})

// Updates the sidebar's count of workspaces waiting for approval.
export function useRefreshPending(): () => void {
  return useContext(PendingContext)
}

export function ConsoleLayout({ children }: { children: ReactNode }) {
  const { organizations } = useAuth()
  const isActive = useIsActive()
  const pending = useLoad('organizations:pending', () => api.listOrganizations({ status: 'pending' }))
  const pendingCount = pending.data?.data.length ?? 0
  type NavItem = { href: string; label: string; icon: typeof HomeIcon; exact?: boolean; badge?: number }
  const groups: { heading?: string; items: NavItem[] }[] = [
    {
      items: [
        { href: routes.admin.overview(), label: 'Home', icon: HomeIcon, exact: true },
        { href: routes.admin.workspaces(), label: 'Workspaces', icon: BuildingOffice2Icon, badge: pendingCount },
        { href: routes.admin.users(), label: 'Users', icon: UsersIcon },
      ],
    },
    {
      heading: 'Compute',
      items: [
        { href: routes.admin.sandboxes(), label: 'Sandboxes', icon: CubeIcon },
        { href: routes.admin.snapshots(), label: 'Snapshots', icon: CameraIcon },
        { href: routes.admin.servers(), label: 'Servers', icon: ServerStackIcon },
      ],
    },
    {
      heading: 'Manage',
      items: [
        { href: routes.admin.plans(), label: 'Plans', icon: RectangleStackIcon },
        { href: routes.admin.events(), label: 'Audit trail', icon: ClipboardDocumentListIcon },
        { href: routes.admin.settings(), label: 'Settings', icon: Cog6ToothIcon },
      ],
    },
  ]
  return (
    <PendingContext.Provider value={pending.refresh}>
      <AppShell
        nav={
          <>
            {groups.map(({ heading, items }) => (
              <SidebarSection key={heading ?? 'main'} aria-label={heading ? undefined : 'Main navigation'}>
                {heading && <SidebarHeading>{heading}</SidebarHeading>}
                {items.map(({ href, label, icon: Icon, exact, badge }) => (
                  <SidebarItem key={href} href={href} current={isActive(href, exact)}>
                    <Icon data-slot="icon" />
                    <SidebarLabel>{label}</SidebarLabel>
                    {badge ? (
                      <Badge color="amber" className="ml-auto" title={`${badge} waiting for approval`}>
                        {badge}
                      </Badge>
                    ) : null}
                  </SidebarItem>
                ))}
              </SidebarSection>
            ))}
            {organizations.length > 0 && (
              <SidebarSection>
                <SidebarItem href={routes.home()}>
                  <UserCircleIcon data-slot="icon" />
                  <SidebarLabel>Workspace portal</SidebarLabel>
                </SidebarItem>
              </SidebarSection>
            )}
          </>
        }
      >
        {children}
      </AppShell>
    </PendingContext.Provider>
  )
}
