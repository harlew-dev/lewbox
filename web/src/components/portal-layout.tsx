import { SidebarHeading, SidebarItem, SidebarLabel, SidebarSection } from '@harlew-dev/ui'
import {
  ChartBarIcon,
  CubeIcon,
  HomeIcon,
  KeyIcon,
  RectangleStackIcon,
} from '@heroicons/react/20/solid'
import type { ReactNode } from 'react'
import { AppShell, useIsActive } from './app-shell'
import { WorkspaceSwitcher } from './workspace-switcher'

export function PortalLayout({ children }: { children: ReactNode }) {
  const isActive = useIsActive()
  const primary = [
    { href: '/', label: 'Home', icon: HomeIcon, exact: true },
    { href: '/sandboxes', label: 'Sandboxes', icon: CubeIcon },
  ]
  const manage = [
    { href: '/keys', label: 'API keys', icon: KeyIcon },
    { href: '/usage', label: 'Usage', icon: ChartBarIcon },
    { href: '/plan', label: 'Plan & Billing', icon: RectangleStackIcon },
  ]
  return (
    <AppShell
      header={<WorkspaceSwitcher className="min-w-0 flex-1" />}
      navbar={<WorkspaceSwitcher navbar className="min-w-0" />}
      nav={
        <>
          <SidebarSection aria-label="Main navigation">
            {primary.map(({ href, label, icon: Icon, exact }) => (
              <SidebarItem key={href} href={href} current={isActive(href, exact)}>
                <Icon data-slot="icon" />
                <SidebarLabel>{label}</SidebarLabel>
              </SidebarItem>
            ))}
          </SidebarSection>
          <SidebarSection>
            <SidebarHeading>Manage</SidebarHeading>
            {manage.map(({ href, label, icon: Icon }) => (
              <SidebarItem key={href} href={href} current={isActive(href)}>
                <Icon data-slot="icon" />
                <SidebarLabel>{label}</SidebarLabel>
              </SidebarItem>
            ))}
          </SidebarSection>
        </>
      }
    >
      {children}
    </AppShell>
  )
}
