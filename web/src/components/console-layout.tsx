import {
  Avatar,
  Dropdown,
  DropdownButton,
  DropdownDivider,
  DropdownItem,
  DropdownLabel,
  DropdownMenu,
  Navbar,
  NavbarSection,
  NavbarSpacer,
  PageTransition,
  Sidebar,
  SidebarBody,
  SidebarFooter,
  SidebarHeader,
  SidebarHeading,
  SidebarItem,
  SidebarLabel,
  SidebarLayout,
  SidebarSection,
  SidebarSpacer,
  SidebarToggle,
  Toaster,
} from '@harlew-dev/ui'
import { ChevronDownIcon, PlusIcon } from '@heroicons/react/16/solid'
import {
  BuildingOffice2Icon,
  BookOpenIcon,
  Cog6ToothIcon,
  HomeIcon,
  KeyIcon,
  ServerStackIcon,
  UserGroupIcon,
  UsersIcon,
} from '@heroicons/react/20/solid'
import { useState, type ReactNode } from 'react'
import { useLocation, useNavigate } from 'react-router'
import type * as api from '../lib/api'
import { useAuth } from '../lib/auth'
import { orgInitials, orgPermissions, useOrganizations } from '../lib/organizations'
import { AccountMenu } from './account-menu'
import { BrandLogo } from './brand-logo'

// The organization switcher, as in Catalyst's sidebar header: the current
// organization, the others, and (for instance admins) all of them.
function OrganizationMenu({
  organizations,
  current,
  instanceAdmin,
}: {
  organizations: api.Organization[]
  current: api.Organization
  instanceAdmin: boolean
}) {
  return (
    <Dropdown>
      <DropdownButton as={SidebarItem}>
        <Avatar initials={orgInitials(current.name)} color="zinc" data-slot="avatar" />
        <SidebarLabel>{current.name}</SidebarLabel>
        <ChevronDownIcon data-slot="icon" />
      </DropdownButton>
      <DropdownMenu anchor="bottom start" className="min-w-(--anchor-width)">
        {organizations.slice(0, 8).map((org) => (
          <DropdownItem key={org.id} href={`/organizations/${org.slug}/members`} selected={org.id === current.id}>
            <Avatar initials={orgInitials(org.name)} color="zinc" data-slot="avatar" />
            <DropdownLabel>{org.name}</DropdownLabel>
          </DropdownItem>
        ))}
        <DropdownDivider />
        <DropdownItem href="/organizations">
          <BuildingOffice2Icon data-slot="icon" />
          <DropdownLabel>{instanceAdmin ? 'All organizations' : 'Your organizations'}</DropdownLabel>
        </DropdownItem>
        {instanceAdmin && (
          <DropdownItem href="/organizations?new=1">
            <PlusIcon data-slot="icon" />
            <DropdownLabel>New organization</DropdownLabel>
          </DropdownItem>
        )}
      </DropdownMenu>
    </Dropdown>
  )
}

export function ConsoleLayout({ children }: { children: ReactNode }) {
  const { user, signOut } = useAuth()
  const { organizations, current } = useOrganizations()
  const location = useLocation()
  const navigate = useNavigate()
  const [signingOut, setSigningOut] = useState(false)
  const instanceAdmin = user?.instance_admin === true

  const isActive = (href: string) =>
    href === '/'
      ? location.pathname === '/'
      : location.pathname === href || location.pathname.startsWith(`${href}/`)

  async function handleSignOut() {
    if (signingOut) return
    setSigningOut(true)
    try {
      await signOut()
      navigate('/login', { replace: true })
    } finally {
      setSigningOut(false)
    }
  }

  const accountMenu = (placement: 'sidebar' | 'navbar') => (
    <AccountMenu
      user={user}
      active={isActive('/account')}
      signingOut={signingOut}
      onSignOut={() => void handleSignOut()}
      placement={placement}
    />
  )

  const orgBase = current ? `/organizations/${current.slug}` : null
  const can = current ? orgPermissions(current, instanceAdmin) : null

  const sidebar = (
    <Sidebar>
      <SidebarHeader>
        <div className="flex items-center justify-between gap-2">
          <BrandLogo href="/" />
          <SidebarToggle />
        </div>
        {current && (
          <SidebarSection>
            <OrganizationMenu organizations={organizations} current={current} instanceAdmin={instanceAdmin} />
          </SidebarSection>
        )}
      </SidebarHeader>
      <SidebarBody>
        <SidebarSection>
          <SidebarItem href="/" current={isActive('/')}>
            <HomeIcon data-slot="icon" />
            <SidebarLabel>Overview</SidebarLabel>
          </SidebarItem>
          <SidebarItem href="/servers" current={isActive('/servers')}>
            <ServerStackIcon data-slot="icon" />
            <SidebarLabel>Servers</SidebarLabel>
          </SidebarItem>
        </SidebarSection>

        {orgBase && can && (
          <SidebarSection>
            <SidebarHeading>Organization</SidebarHeading>
            <SidebarItem href={`${orgBase}/members`} current={isActive(`${orgBase}/members`)}>
              <UserGroupIcon data-slot="icon" />
              <SidebarLabel>Members</SidebarLabel>
            </SidebarItem>
            {can.keys && (
              <SidebarItem href={`${orgBase}/api-keys`} current={isActive(`${orgBase}/api-keys`)}>
                <KeyIcon data-slot="icon" />
                <SidebarLabel>API keys</SidebarLabel>
              </SidebarItem>
            )}
            <SidebarItem href={`${orgBase}/settings`} current={isActive(`${orgBase}/settings`)}>
              <Cog6ToothIcon data-slot="icon" />
              <SidebarLabel>Settings</SidebarLabel>
            </SidebarItem>
          </SidebarSection>
        )}

        {instanceAdmin ? (
          <SidebarSection>
            <SidebarHeading>Instance</SidebarHeading>
            <SidebarItem href="/organizations" current={location.pathname === '/organizations'}>
              <BuildingOffice2Icon data-slot="icon" />
              <SidebarLabel>Organizations</SidebarLabel>
            </SidebarItem>
            <SidebarItem href="/users" current={isActive('/users')}>
              <UsersIcon data-slot="icon" />
              <SidebarLabel>Users</SidebarLabel>
            </SidebarItem>
          </SidebarSection>
        ) : (
          !current && (
            <SidebarSection>
              <SidebarItem href="/organizations" current={location.pathname === '/organizations'}>
                <BuildingOffice2Icon data-slot="icon" />
                <SidebarLabel>Organizations</SidebarLabel>
              </SidebarItem>
            </SidebarSection>
          )
        )}

        <SidebarSpacer />
        <SidebarSection>
          <SidebarItem href="/docs">
            <BookOpenIcon data-slot="icon" />
            <SidebarLabel>Documentation</SidebarLabel>
          </SidebarItem>
        </SidebarSection>
      </SidebarBody>
      <SidebarFooter>
        <SidebarSection>{accountMenu('sidebar')}</SidebarSection>
      </SidebarFooter>
    </Sidebar>
  )

  const navbar = (
    <Navbar>
      <NavbarSpacer />
      <NavbarSection>{accountMenu('navbar')}</NavbarSection>
    </Navbar>
  )

  return (
    <SidebarLayout
      navbar={navbar}
      sidebar={sidebar}
      pathname={location.pathname}
      storageKey="lewbox:sidebar"
    >
      <PageTransition key={location.pathname}>{children}</PageTransition>
      <Toaster />
    </SidebarLayout>
  )
}
