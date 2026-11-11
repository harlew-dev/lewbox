import {
  Avatar,
  Dropdown,
  DropdownButton,
  DropdownDivider,
  DropdownItem,
  DropdownLabel,
  DropdownMenu,
  SidebarItem,
  SidebarLabel,
  SidebarSection,
} from '@harlew-dev/ui'
import { ChevronDownIcon, WrenchScrewdriverIcon } from '@heroicons/react/16/solid'
import {
  ChartBarIcon,
  Cog6ToothIcon,
  CreditCardIcon,
  CubeIcon,
  HomeIcon,
  KeyIcon,
  UserGroupIcon,
} from '@heroicons/react/20/solid'
import type { ReactNode } from 'react'
import { useAuth } from '../lib/auth'
import { useInstance } from '../lib/instance'
import { orgInitials, useOrganizations } from '../lib/organizations'
import { AppShell, useIsActive } from './app-shell'

// Shown when there is a choice: several organizations, or the operator's
// console for an instance admin who is also a member.
function OrganizationSwitcher() {
  const { user } = useAuth()
  const { organizations, current, select } = useOrganizations()
  const admin = user?.instance_admin === true
  if (!current || (organizations.length < 2 && !admin)) return null
  return (
    <SidebarSection>
      <Dropdown>
        <DropdownButton as={SidebarItem}>
          <Avatar initials={orgInitials(current.name)} color="zinc" data-slot="avatar" />
          <SidebarLabel>{current.name}</SidebarLabel>
          <ChevronDownIcon data-slot="icon" />
        </DropdownButton>
        <DropdownMenu anchor="bottom start" className="min-w-(--anchor-width)">
          {organizations.map((org) => (
            <DropdownItem key={org.id} onClick={() => select(org.slug)} selected={org.id === current.id}>
              <Avatar initials={orgInitials(org.name)} color="zinc" data-slot="avatar" />
              <DropdownLabel>{org.name}</DropdownLabel>
            </DropdownItem>
          ))}
          {admin && (
            <>
              <DropdownDivider />
              <DropdownItem href="/admin">
                <WrenchScrewdriverIcon data-slot="icon" />
                <DropdownLabel>Operator console</DropdownLabel>
              </DropdownItem>
            </>
          )}
        </DropdownMenu>
      </Dropdown>
    </SidebarSection>
  )
}

export function PortalLayout({ children }: { children: ReactNode }) {
  const { billing } = useInstance()
  const isActive = useIsActive()
  const items = [
    { href: '/', label: 'Home', icon: HomeIcon, exact: true },
    { href: '/sandboxes', label: 'Sandboxes', icon: CubeIcon },
    { href: '/keys', label: 'API keys', icon: KeyIcon },
    { href: '/usage', label: 'Usage', icon: ChartBarIcon },
    ...(billing !== 'off' ? [{ href: '/billing', label: 'Billing', icon: CreditCardIcon }] : []),
    { href: '/team', label: 'Team', icon: UserGroupIcon },
    { href: '/settings', label: 'Settings', icon: Cog6ToothIcon },
  ]
  return (
    <AppShell
      header={<OrganizationSwitcher />}
      nav={
        <SidebarSection>
          {items.map(({ href, label, icon: Icon, exact }) => (
            <SidebarItem key={href} href={href} current={isActive(href, exact)}>
              <Icon data-slot="icon" />
              <SidebarLabel>{label}</SidebarLabel>
            </SidebarItem>
          ))}
        </SidebarSection>
      }
    >
      {children}
    </AppShell>
  )
}
