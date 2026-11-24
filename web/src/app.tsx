import { ListSkeleton } from '@harlew-dev/ui'
import { lazy, Suspense } from 'react'
import { Navigate, Outlet, Route, Routes, useLocation, useParams } from 'react-router'
import { ConsoleLayout } from './components/console-layout'
import { DocsLayout } from './components/docs-layout'
import { PortalLayout } from './components/portal-layout'
import { RequireAdmin, RequireAuth, useAuth } from './lib/auth'
import { LoginPage } from './pages/login'
import { NotFoundPage } from './pages/not-found'
import { PortalRoute } from './pages/portal/portal-route'
import { SettingsLayout } from './pages/portal/settings/layout'
import { SignupPage } from './pages/signup'
import { AccountPage } from './pages/account'
import { WorkspaceDetailPage } from './pages/admin/workspace-detail'
import { WorkspacesPage } from './pages/admin/workspaces'
import { OverviewPage } from './pages/admin/overview'
import { PlansPage } from './pages/admin/plans'
import { FleetSandboxesPage } from './pages/admin/sandboxes'
import { FleetSnapshotsPage } from './pages/admin/snapshots'
import { AdminSettingsPage } from './pages/admin/settings'
import { OnboardingPage } from './pages/onboarding/onboarding'
import { GettingStartedPage } from './pages/onboarding/getting-started'
import { SetupPage } from './pages/setup'
import { HomePage } from './pages/portal/home'
import { KeysPage } from './pages/portal/keys'
import { SandboxesPage } from './pages/portal/sandboxes'
import { PlanPage } from './pages/portal/plan'
import { UsagePage } from './pages/portal/usage'
import { SettingsGeneralPage } from './pages/portal/settings/general'
import { SettingsMembersPage } from './pages/portal/settings/members'
import { SettingsKeysPage } from './pages/portal/settings/api-keys'
import { ClusterDetailPage } from './pages/servers/cluster-detail'
import { ServersPage } from './pages/servers/servers'
import { UserDetailPage } from './pages/users/user-detail'
import { UsersPage } from './pages/users/users'

// The app's pages are in the entry bundle, so moving between them never
// waits for a download: a page draws its header and layout straight away
// and only its data shows a placeholder. The documentation is the exception:
// its markdown renderer is large and its layout is separate, so it loads on
// demand behind a skeleton.
const docsFallback = <ListSkeleton label="Loading" rows={4} />
const DocsLazy = lazy(() => import('./pages/docs').then((m) => ({ default: m.DocsPage })))
const PlatformDocsLazy = lazy(() => import('./pages/docs').then((m) => ({ default: m.PlatformDocsPage })))

function DocsPage() {
  return (
    <Suspense fallback={docsFallback}>
      <DocsLazy />
    </Suspense>
  )
}

function PlatformDocsPage() {
  return (
    <Suspense fallback={docsFallback}>
      <PlatformDocsLazy />
    </Suspense>
  )
}

// The customer portal. An instance admin who belongs to no workspace
// has nothing here and goes to the console; everyone else without one goes
// to onboarding to create it.
function Portal() {
  const { user, organizations } = useAuth()
  if (organizations.length === 0) {
    return <Navigate to={user?.instance_admin ? '/admin' : '/onboarding'} replace />
  }
  return <PortalRoute />
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
        element={<SignupPage />}
      />
      <Route
        path="/setup/:token"
        element={<SetupPage />}
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
          element={<OnboardingPage />}
        />
        <Route
          path="/onboarding/getting-started"
          element={<GettingStartedPage />}
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
