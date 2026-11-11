import {
  Button,
  DocsLayout as Layout,
  ListSkeleton,
  Navbar,
  NavbarItem,
  NavbarSection,
  NavbarSpacer,
  Notice,
  SideNav,
  SideNavItem,
  SideNavSection,
  Toaster,
} from '@harlew-dev/ui'
import { ArrowUpRightIcon, HomeIcon } from '@heroicons/react/20/solid'
import { Outlet, useLocation } from 'react-router'
import { BrandLogo } from './brand-logo'
import { homeFor, useAuth } from '../lib/auth'
import * as api from '../lib/api'
import { useLoad } from '../lib/use-load'

function docsPath(pathname: string): string {
  return pathname.replace(/^\/docs\/?/, '').replace(/\/+$/, '')
}

function navHref(path: string): string {
  return `/docs${path ? `/${path}` : ''}`
}

export function DocsLayout() {
  const { status, user } = useAuth()
  const location = useLocation()
  const current = docsPath(location.pathname)
  const nav = useLoad(() => api.docsNav(), [])
  const signedIn = status === 'authenticated'

  const sideNav = (
    <SideNav aria-label="Documentation">
      <SideNavSection plain>
        <SideNavItem href="/docs" current={current === ''}>
          <HomeIcon data-slot="icon" />
          Introduction
        </SideNavItem>
      </SideNavSection>

      {nav.loading && !nav.data ? (
        <ListSkeleton label="Loading documentation navigation" rows={6} />
      ) : nav.error.message ? (
        <div className="space-y-2">
          <Notice variant="error">Could not load the documentation navigation.</Notice>
          <Button plain onClick={nav.reload}>
            Try again
          </Button>
        </div>
      ) : (
        (nav.data?.groups ?? []).map((group) => (
          <SideNavSection key={group.title} title={group.title}>
            {group.items.map((item) =>
              item.external ? (
                <SideNavItem
                  key={item.title}
                  href={`https://github.com/harlew-dev/lewbox/blob/main/${item.external}`}
                  target="_blank"
                  rel="noreferrer"
                >
                  {item.title}
                  <ArrowUpRightIcon className="size-4 shrink-0 fill-zinc-400 dark:fill-zinc-500" />
                </SideNavItem>
              ) : (
                <SideNavItem
                  key={item.title}
                  href={navHref(item.path)}
                  current={current === item.path}
                  title={item.description}
                >
                  {item.title}
                </SideNavItem>
              ),
            )}
          </SideNavSection>
        ))
      )}
    </SideNav>
  )

  const navbar = (
    <Navbar>
      <NavbarSpacer />
      <NavbarSection>
        {signedIn ? (
          <NavbarItem href={homeFor(user)}>{user?.instance_admin ? 'Console' : 'Dashboard'}</NavbarItem>
        ) : (
          <NavbarItem href="/login">Sign in</NavbarItem>
        )}
      </NavbarSection>
    </Navbar>
  )

  return (
    <Layout
      logo={<BrandLogo href="/docs" />}
      navbar={navbar}
      nav={sideNav}
      pathname={location.pathname}
    >
      <Outlet />
      <Toaster />
    </Layout>
  )
}
