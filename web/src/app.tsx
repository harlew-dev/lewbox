import { Navigate, Outlet, Route, Routes, useLocation } from 'react-router'
import { ConsoleLayout } from './components/console-layout'
import { DocsLayout } from './components/docs-layout'
import { PortalLayout } from './components/portal-layout'
import { RequireAdmin, RequireAuth, useAuth } from './lib/auth'
import { AccountPage } from './pages/account'
import { CustomerDetailPage } from './pages/admin/customer-detail'
import { CustomersPage } from './pages/admin/customers'
import { OverviewPage } from './pages/admin/overview'
import { PlansPage } from './pages/admin/plans'
import { FleetSandboxesPage } from './pages/admin/sandboxes'
import { AdminSettingsPage } from './pages/admin/settings'
import { DocsPage } from './pages/docs'
import { LoginPage } from './pages/login'
import { NotFoundPage } from './pages/not-found'
import { BillingPage } from './pages/portal/billing'
import { HomePage } from './pages/portal/home'
import { KeysPage } from './pages/portal/keys'
import { PortalRoute } from './pages/portal/portal-route'
import { SandboxesPage } from './pages/portal/sandboxes'
import { SettingsPage } from './pages/portal/settings'
import { TeamPage } from './pages/portal/team'
import { UsagePage } from './pages/portal/usage'
import { ClusterDetailPage } from './pages/servers/cluster-detail'
import { ServersPage } from './pages/servers/servers'
import { SignupPage } from './pages/signup'
import { UserDetailPage } from './pages/users/user-detail'
import { UsersPage } from './pages/users/users'

// The customer portal. An instance admin who belongs to no organization
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
          <Route path="/keys" element={<KeysPage />} />
          <Route path="/usage" element={<UsagePage />} />
          <Route path="/billing" element={<BillingPage />} />
          <Route path="/team" element={<TeamPage />} />
          <Route path="/settings" element={<SettingsPage />} />
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
