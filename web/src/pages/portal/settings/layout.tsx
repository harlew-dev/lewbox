import { Divider, Heading, Navbar, NavbarItem, NavbarSection } from '@harlew-dev/ui'
import { Outlet, useLocation } from 'react-router'
import { usePortalOrg } from '../portal-route'

// Workspace settings' heading and tabs around General, Members and API keys.
export function SettingsLayout() {
  const context = usePortalOrg()
  const { can } = context
  const { pathname } = useLocation()
  const tabs = [
    { label: 'General', href: '/settings' },
    { label: 'Members', href: '/settings/members' },
    ...(can.keys ? [{ label: 'Management API keys', href: '/settings/api-keys' }] : []),
  ]

  return (
    <div className="mx-auto max-w-4xl">
      <Heading>Workspace settings</Heading>
      <Navbar aria-label="Workspace settings" className="mt-4">
        <NavbarSection className="-ml-2">
          {tabs.map((tab) => (
            <NavbarItem
              key={tab.href}
              href={tab.href}
              current={pathname === tab.href}
              aria-current={pathname === tab.href ? 'page' : undefined}
            >
              {tab.label}
            </NavbarItem>
          ))}
        </NavbarSection>
      </Navbar>
      <Divider className="my-10 mt-6" />
      <Outlet context={context} />
    </div>
  )
}
