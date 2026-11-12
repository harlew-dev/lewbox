import { ListSkeleton } from '@harlew-dev/ui'
import { lazy, Suspense, useEffect, useState, type ComponentType, type ReactNode } from 'react'
import { Navigate, Outlet, Route, Routes, useLocation, useParams } from 'react-router'
import { ConsoleLayout } from './components/console-layout'
import { DocsLayout } from './components/docs-layout'
import { CenteredLoading } from './components/loading'
import { PortalLayout } from './components/portal-layout'
import { RequireAdmin, RequireAuth, useAuth } from './lib/auth'
import { LoginPage } from './pages/login'
import { NotFoundPage } from './pages/not-found'
import { PortalRoute } from './pages/portal/portal-route'
import { SettingsLayout } from './pages/portal/settings/layout'
import { SignupPage } from './pages/signup'

// Pages other than sign-in and sign-up load on demand, so the entry chunk
// holds only the shell; charts (recharts) and markdown load with the pages
// that use them. The layout stays on screen while a page's chunk downloads.
// Standalone pages (onboarding, setup) draw their own layout, so they pass a
// fallback in the same centred frame instead of the dashboard's skeleton.
//
// `preload()` fetches a page's chunk ahead of time. A page whose chunk has
// already arrived renders straight away: React.lazy would still suspend once
// on first render, and flash the fallback between two steps.
type LazyPage = ComponentType & { preload: () => void }

function lazyPage<K extends string>(
  load: () => Promise<Record<K, ComponentType>>,
  name: K,
  fallback: ReactNode = <ListSkeleton label="Loading" rows={4} />,
): LazyPage {
  let loaded: ComponentType | undefined
  let loading: Promise<ComponentType> | undefined
  const loadPage = () =>
    (loading ??= load().then(
      (m) => (loaded = m[name]),
      (failure: unknown) => {
        loading = undefined
        throw failure
      },
    ))
  const Lazy = lazy(() => loadPage().then((page) => ({ default: page })))
  function Page() {
    // Decided once per mount, so a page never swaps from Lazy to the loaded
    // component (and remounts) while it is open.
    const [Ready] = useState(() => loaded)
    if (Ready) return <Ready />
    return (
      <Suspense fallback={fallback}>
        <Lazy />
      </Suspense>
    )
  }
  Page.preload = () => void loadPage().catch(() => {})
  return Page
}

// Loads the chunks of the pages a step leads to while it is open, so moving
// on doesn't wait for a download.
function Preload({ pages, children }: { pages: LazyPage[]; children: ReactNode }) {
  useEffect(() => {
    // The pages are module-level constants, so once per mount is enough.
    for (const page of pages) page.preload()
  }, [])
  return <>{children}</>
}

const centred = <CenteredLoading />

const AccountPage = lazyPage(() => import('./pages/account'), 'AccountPage')
const WorkspaceDetailPage = lazyPage(() => import('./pages/admin/workspace-detail'), 'WorkspaceDetailPage')
const WorkspacesPage = lazyPage(() => import('./pages/admin/workspaces'), 'WorkspacesPage')
const OverviewPage = lazyPage(() => import('./pages/admin/overview'), 'OverviewPage')
const PlansPage = lazyPage(() => import('./pages/admin/plans'), 'PlansPage')
const FleetSandboxesPage = lazyPage(() => import('./pages/admin/sandboxes'), 'FleetSandboxesPage')
const FleetSnapshotsPage = lazyPage(() => import('./pages/admin/snapshots'), 'FleetSnapshotsPage')
const AdminSettingsPage = lazyPage(() => import('./pages/admin/settings'), 'AdminSettingsPage')
const DocsPage = lazyPage(() => import('./pages/docs'), 'DocsPage')
const PlatformDocsPage = lazyPage(() => import('./pages/docs'), 'PlatformDocsPage')
const OnboardingPage = lazyPage(() => import('./pages/onboarding/onboarding'), 'OnboardingPage', centred)
const GettingStartedPage = lazyPage(() => import('./pages/onboarding/getting-started'), 'GettingStartedPage', centred)
const SetupPage = lazyPage(() => import('./pages/setup'), 'SetupPage', centred)
const HomePage = lazyPage(() => import('./pages/portal/home'), 'HomePage')
const KeysPage = lazyPage(() => import('./pages/portal/keys'), 'KeysPage')
const SandboxesPage = lazyPage(() => import('./pages/portal/sandboxes'), 'SandboxesPage')
const PlanPage = lazyPage(() => import('./pages/portal/plan'), 'PlanPage')
const UsagePage = lazyPage(() => import('./pages/portal/usage'), 'UsagePage')
const SettingsGeneralPage = lazyPage(() => import('./pages/portal/settings/general'), 'SettingsGeneralPage')
const SettingsMembersPage = lazyPage(() => import('./pages/portal/settings/members'), 'SettingsMembersPage')
const SettingsKeysPage = lazyPage(() => import('./pages/portal/settings/api-keys'), 'SettingsKeysPage')
const ClusterDetailPage = lazyPage(() => import('./pages/servers/cluster-detail'), 'ClusterDetailPage')
const ServersPage = lazyPage(() => import('./pages/servers/servers'), 'ServersPage')
const UserDetailPage = lazyPage(() => import('./pages/users/user-detail'), 'UserDetailPage')
const UsersPage = lazyPage(() => import('./pages/users/users'), 'UsersPage')

// The customer portal. An instance admin who belongs to no workspace
// has nothing here and goes to the console; everyone else without one goes
// to onboarding to create it.
function Portal() {
  const { user, organizations } = useAuth()
  if (organizations.length === 0) {
    return <Navigate to={user?.instance_admin ? '/admin' : '/onboarding'} replace />
  }
  return (
    <PortalLayout>
      <PortalRoute />
    </PortalLayout>
  )
}

function Console() {
  return (
    <RequireAdmin>
      <ConsoleLayout>
        <Outlet />
      </ConsoleLayout>
    </RequireAdmin>
  )
}

function Account() {
  const { user } = useAuth()
  const Layout = user?.instance_admin ? ConsoleLayout : PortalLayout
  return (
    <Layout>
      <AccountPage />
    </Layout>
  )
}

// Where a console page lived before the console moved under /admin.
export function legacyPath(pathname: string, search: string): string {
  const [, first = '', ...rest] = pathname.split('/')
  const tail = rest.filter(Boolean).join('/')
  switch (first) {
    case 'clusters':
      return tail ? `/admin/servers/clusters/${tail}` : '/admin/servers'
    case 'nodes':
      return '/admin/servers?tab=nodes'
    case 'organizations':
      return tail ? `/admin/workspaces/${tail.split('/')[0]}` : '/admin/workspaces'
    default:
      return `/admin${pathname}${search}`
  }
}

function Legacy() {
  const { user } = useAuth()
  const location = useLocation()
  if (!user?.instance_admin) return <NotFoundPage />
  return <Navigate to={legacyPath(location.pathname, location.search)} replace />
}

// The console's old customer pages became workspaces.
function LegacyCustomer() {
  const { org = '' } = useParams()
  return <Navigate to={`/admin/workspaces/${encodeURIComponent(org)}`} replace />
}

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        path="/signup"
        element={
          <Preload pages={[OnboardingPage]}>
            <SignupPage />
          </Preload>
        }
      />
      <Route
        path="/setup/:token"
        element={
          <Preload pages={[OnboardingPage]}>
            <SetupPage />
          </Preload>
        }
      />

      <Route element={<DocsLayout book="consumer" />}>
        <Route path="/docs" element={<DocsPage />} />
        <Route path="/docs/*" element={<DocsPage />} />
      </Route>

      <Route element={<DocsLayout book="platform" />}>
        <Route path="/platform" element={<PlatformDocsPage />} />
        <Route path="/platform/*" element={<PlatformDocsPage />} />
      </Route>

      <Route
        element={
          <RequireAuth>
            <Outlet />
          </RequireAuth>
        }
      >
        <Route
          path="/onboarding"
          element={
            <Preload pages={[GettingStartedPage]}>
              <OnboardingPage />
            </Preload>
          }
        />
        <Route
          path="/onboarding/getting-started"
          element={
            <Preload pages={[HomePage]}>
              <GettingStartedPage />
            </Preload>
          }
        />

        <Route element={<Portal />}>
          <Route index element={<HomePage />} />
          <Route path="/sandboxes" element={<SandboxesPage />} />
          <Route path="/snapshots" element={<Navigate to="/sandboxes?tab=snapshots" replace />} />
          <Route path="/keys" element={<KeysPage />} />
          <Route path="/usage" element={<UsagePage />} />
          <Route path="/plan" element={<PlanPage />} />
          <Route path="/settings" element={<SettingsLayout />}>
            <Route index element={<SettingsGeneralPage />} />
            <Route path="members" element={<SettingsMembersPage />} />
            <Route path="api-keys" element={<SettingsKeysPage />} />
          </Route>
          <Route path="/team" element={<Navigate to="/settings/members" replace />} />
        </Route>

        <Route path="/admin" element={<Console />}>
          <Route index element={<OverviewPage />} />
          <Route path="workspaces" element={<WorkspacesPage />} />
          <Route path="workspaces/:org" element={<WorkspaceDetailPage />} />
          <Route path="customers" element={<Navigate to="/admin/workspaces" replace />} />
          <Route path="customers/:org" element={<LegacyCustomer />} />
          <Route path="sandboxes" element={<FleetSandboxesPage />} />
          <Route path="snapshots" element={<FleetSnapshotsPage />} />
          <Route path="plans" element={<PlansPage />} />
          <Route path="servers" element={<ServersPage />} />
          <Route path="servers/clusters/:ref" element={<ClusterDetailPage />} />
          <Route path="users" element={<UsersPage />} />
          <Route path="users/:ref" element={<UserDetailPage />} />
          <Route path="settings" element={<AdminSettingsPage />} />
          <Route path="*" element={<NotFoundPage />} />
        </Route>

        <Route path="/account" element={<Account />} />

        {['/servers/*', '/clusters/*', '/nodes', '/users/*', '/organizations/*'].map((path) => (
          <Route key={path} path={path} element={<Legacy />} />
        ))}
      </Route>

      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  )
}
