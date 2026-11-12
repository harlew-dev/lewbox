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
  Text,
  Toaster,
} from '@harlew-dev/ui'
import { ArrowUpRightIcon, HomeIcon } from '@heroicons/react/20/solid'
import { Outlet, useLocation } from 'react-router'
import { BrandLogo } from './brand-logo'
import { homeFor, useAuth } from '../lib/auth'
import * as api from '../lib/api'
import { useLoad } from '../lib/use-load'

const books: Record<api.DocsBook, { prefix: string; title: string; other: api.DocsBook }> = {
  consumer: { prefix: '/docs', title: 'Docs', other: 'platform' },
  platform: { prefix: '/platform', title: 'Platform docs', other: 'consumer' },
}

function docsPath(pathname: string, prefix: string): string {
  return pathname.replace(new RegExp(`^${prefix}/?`), '').replace(/\/+$/, '')
}

function navHref(prefix: string, path: string): string {
  return `${prefix}${path ? `/${path}` : ''}`
}

// A nav item outside both books names a file in the repository. The Go
// server writes it relative to the book's README, so remove any "..".
function githubHref(path: string): string {
  const parts: string[] = []
  for (const segment of path.split('/')) {
    if (segment === '' || segment === '.') continue
    if (segment === '..') parts.pop()
    else parts.push(segment)
  }
  return `https://github.com/harlew-dev/lewbox/blob/main/${parts.join('/')}`
}

export function DocsLayout({ book = 'consumer' }: { book?: api.DocsBook }) {
  const { status, user } = useAuth()
  const location = useLocation()
  const { prefix, title, other } = books[book]
  const current = docsPath(location.pathname, prefix)
  const nav = useLoad(() => api.docsNav(book), [book])
  const signedIn = status === 'authenticated'

  const sideNav = (
    <SideNav aria-label={title}>
      <SideNavSection plain>
        <SideNavItem href={prefix} current={current === ''}>
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
                  href={githubHref(item.external)}
                  target="_blank"
                  rel="noreferrer"
                >
                  {item.title}
                  <ArrowUpRightIcon className="size-4 shrink-0 fill-zinc-400 dark:fill-zinc-500" />
                </SideNavItem>
              ) : (
                <SideNavItem
                  key={item.title}
                  href={navHref(prefix, item.path)}
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
        {signedIn && user?.instance_admin && (
          <NavbarItem href={books[other].prefix}>{books[other].title}</NavbarItem>
        )}
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
      logo={
        <div className="flex items-center gap-3">
          <BrandLogo href={prefix} />
          <Text className="text-sm font-medium text-zinc-500 dark:text-zinc-400">{title}</Text>
        </div>
      }
      navbar={navbar}
      nav={sideNav}
      pathname={location.pathname}
    >
      <Outlet />
      <Toaster />
    </Layout>
  )
}
