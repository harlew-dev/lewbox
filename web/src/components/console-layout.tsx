import { Badge, SidebarItem, SidebarLabel, SidebarSection } from '@harlew-dev/ui'
import {
  BuildingOffice2Icon,
  Cog6ToothIcon,
  CubeIcon,
  HomeIcon,
  RectangleStackIcon,
  ServerStackIcon,
  UserCircleIcon,
  UsersIcon,
} from '@heroicons/react/20/solid'
import { createContext, useContext, type ReactNode } from 'react'
import * as api from '../lib/api'
import { useAuth } from '../lib/auth'
import { useLoad } from '../lib/use-load'
import { AppShell, useIsActive } from './app-shell'

const PendingContext = createContext<() => void>(() => {})

// Updates the sidebar's count of customers waiting for approval.
export function useRefreshPending(): () => void {
  return useContext(PendingContext)
}

export function ConsoleLayout({ children }: { children: ReactNode }) {
  const { organizations } = useAuth()
  const isActive = useIsActive()
  const pending = useLoad(() => api.listOrganizations({ status: 'pending' }), [])
  const pendingCount = pending.data?.data.length ?? 0
  const items = [
    { href: '/admin', label: 'Overview', icon: HomeIcon, exact: true },
    { href: '/admin/customers', label: 'Customers', icon: BuildingOffice2Icon, badge: pendingCount },
    { href: '/admin/sandboxes', label: 'Sandboxes', icon: CubeIcon },
    { href: '/admin/plans', label: 'Plans', icon: RectangleStackIcon },
    { href: '/admin/servers', label: 'Servers', icon: ServerStackIcon },
    { href: '/admin/users', label: 'Users', icon: UsersIcon },
    { href: '/admin/settings', label: 'Settings', icon: Cog6ToothIcon },
  ]
  return (
    <PendingContext.Provider value={pending.refresh}>
      <AppShell
        nav={
          <>
            <SidebarSection>
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
            {organizations.length > 0 && (
              <SidebarSection>
                <SidebarItem href="/">
                  <UserCircleIcon data-slot="icon" />
                  <SidebarLabel>Customer portal</SidebarLabel>
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
