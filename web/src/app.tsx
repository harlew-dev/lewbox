import { ListSkeleton } from '@harlew-dev/ui'
import { lazy, Suspense, type ComponentType } from 'react'
import { Navigate, Outlet, Route, Routes, useLocation } from 'react-router'
import { ConsoleLayout } from './components/console-layout'
import { DocsLayout } from './components/docs-layout'
import { PortalLayout } from './components/portal-layout'
import { RequireAdmin, RequireAuth, useAuth } from './lib/auth'
import { LoginPage } from './pages/login'
import { NotFoundPage } from './pages/not-found'
import { PortalRoute } from './pages/portal/portal-route'
import { SignupPage } from './pages/signup'

// Pages other than sign-in and sign-up load on demand, so the entry chunk
// holds only the shell; charts (recharts) and markdown load with the pages
// that use them. The layout stays on screen while a page's chunk downloads.
function lazyPage<K extends string>(load: () => Promise<Record<K, ComponentType>>, name: K) {
  const Lazy = lazy<ComponentType>(() => load().then((m) => ({ default: m[name] })))
  return function Page() {
    return (
      <Suspense fallback={<ListSkeleton label="Loading" rows={4} />}>
        <Lazy />
      </Suspense>
    )
  }
}

const AccountPage = lazyPage(() => import('./pages/account'), 'AccountPage')
const CustomerDetailPage = lazyPage(() => import('./pages/admin/customer-detail'), 'CustomerDetailPage')
const CustomersPage = lazyPage(() => import('./pages/admin/customers'), 'CustomersPage')
const OverviewPage = lazyPage(() => import('./pages/admin/overview'), 'OverviewPage')
const PlansPage = lazyPage(() => import('./pages/admin/plans'), 'PlansPage')
const FleetSandboxesPage = lazyPage(() => import('./pages/admin/sandboxes'), 'FleetSandboxesPage')
const AdminSettingsPage = lazyPage(() => import('./pages/admin/settings'), 'AdminSettingsPage')
const DocsPage = lazyPage(() => import('./pages/docs'), 'DocsPage')
const HomePage = lazyPage(() => import('./pages/portal/home'), 'HomePage')
const KeysPage = lazyPage(() => import('./pages/portal/keys'), 'KeysPage')
const SandboxesPage = lazyPage(() => import('./pages/portal/sandboxes'), 'SandboxesPage')
const TeamPage = lazyPage(() => import('./pages/portal/team'), 'TeamPage')
const UsagePage = lazyPage(() => import('./pages/portal/usage'), 'UsagePage')
const ClusterDetailPage = lazyPage(() => import('./pages/servers/cluster-detail'), 'ClusterDetailPage')
const ServersPage = lazyPage(() => import('./pages/servers/servers'), 'ServersPage')
const UserDetailPage = lazyPage(() => import('./pages/users/user-detail'), 'UserDetailPage')
const UsersPage = lazyPage(() => import('./pages/users/users'), 'UsersPage')

// The customer portal. An instance admin who belongs to no workspace
// has nothing here and goes to the console.
function Portal() {
  const { user, organizations } = useAuth()
  if (user?.instance_admin && organizations.length === 0) return <Navigate to="/admin" replace />
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
      return tail ? `/admin/customers/${tail.split('/')[0]}` : '/admin/customers'
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

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/signup" element={<SignupPage />} />

      <Route element={<DocsLayout />}>
        <Route path="/docs" element={<DocsPage />} />
        <Route path="/docs/*" element={<DocsPage />} />
      </Route>

      <Route
        element={
          <RequireAuth>
            <Outlet />
          </RequireAuth>
        }
      >
        <Route element={<Portal />}>
          <Route index element={<HomePage />} />
          <Route path="/sandboxes" element={<SandboxesPage />} />
          <Route path="/snapshots" element={<Navigate to="/sandboxes?tab=snapshots" replace />} />
          <Route path="/keys" element={<KeysPage />} />
          <Route path="/usage" element={<UsagePage />} />
          <Route path="/team" element={<TeamPage />} />
          <Route path="/settings" element={<Navigate to="/team" replace />} />
        </Route>

        <Route path="/admin" element={<Console />}>
          <Route index element={<OverviewPage />} />
          <Route path="customers" element={<CustomersPage />} />
          <Route path="customers/:org" element={<CustomerDetailPage />} />
          <Route path="sandboxes" element={<FleetSandboxesPage />} />
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
