import {
  Navbar,
  NavbarSection,
  NavbarSpacer,
  PageTransition,
  Sidebar,
  SidebarBody,
  SidebarFooter,
  SidebarHeader,
  SidebarItem,
  SidebarLabel,
  SidebarLayout,
  SidebarSection,
  SidebarSpacer,
  SidebarToggle,
  Toaster,
} from '@harlew-dev/ui'
import { BookOpenIcon } from '@heroicons/react/20/solid'
import { useState, type ReactNode } from 'react'
import { useLocation, useNavigate } from 'react-router'
import { homeFor, useAuth } from '../lib/auth'
import { AccountMenu } from './account-menu'
import { BrandLogo } from './brand-logo'

// Whether a sidebar link is the current page. `exact` for a section's home.
export function useIsActive() {
  const { pathname } = useLocation()
  return (href: string, exact = false) =>
    exact ? pathname === href : pathname === href || pathname.startsWith(`${href}/`)
}

// The sidebar layout both sides of the app share: the logo (or `header` in
// its place), the side's `nav`, Docs, and the account menu. `navbar` sits
// beside the account menu on phones.
export function AppShell({
  header,
  navbar,
  nav,
  children,
}: {
  header?: ReactNode
  navbar?: ReactNode
  nav: ReactNode
  children: ReactNode
}) {
  const { user, signOut } = useAuth()
  const location = useLocation()
  const navigate = useNavigate()
  const isActive = useIsActive()
  const [signingOut, setSigningOut] = useState(false)

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

  const sidebar = (
    <Sidebar>
      <SidebarHeader>
        <div className="flex items-center justify-between gap-2">
          {header ?? <BrandLogo href={homeFor(user)} />}
          <SidebarToggle />
        </div>
      </SidebarHeader>
      <SidebarBody>
        {nav}
        <SidebarSpacer />
        <SidebarSection>
          <SidebarItem href={user?.instance_admin ? '/platform' : '/docs'}>
            <BookOpenIcon data-slot="icon" />
            <SidebarLabel>Docs</SidebarLabel>
          </SidebarItem>
        </SidebarSection>
      </SidebarBody>
      <SidebarFooter>
        <SidebarSection>{accountMenu('sidebar')}</SidebarSection>
      </SidebarFooter>
    </Sidebar>
  )

  const navbarElement = (
    <Navbar>
      <NavbarSpacer />
      <NavbarSection className="min-w-0">
        {navbar}
        {accountMenu('navbar')}
      </NavbarSection>
    </Navbar>
  )

  return (
    <SidebarLayout navbar={navbarElement} sidebar={sidebar} pathname={location.pathname} storageKey="lewbox:sidebar">
      <PageTransition key={location.pathname}>{children}</PageTransition>
      <Toaster />
    </SidebarLayout>
  )
}
