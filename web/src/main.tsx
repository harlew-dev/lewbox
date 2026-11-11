import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Navigate, Outlet, Route, Routes, useParams } from 'react-router'
import './styles/app.css'
import { AppLinkProvider } from './components/app-link'
import { ConsoleLayout } from './components/console-layout'
import { DocsLayout } from './components/docs-layout'
import { AuthProvider, RequireAdmin, RequireAuth } from './lib/auth'
import { OrganizationsProvider } from './lib/organizations'
import { AccountPage } from './pages/account'
import { DocsPage } from './pages/docs'
import { LoginPage } from './pages/login'
import { NotFoundPage } from './pages/not-found'
import { ApiKeysPage } from './pages/organizations/api-keys'
import { MembersPage } from './pages/organizations/members'
import { OrganizationRoute } from './pages/organizations/organization-route'
import { OrganizationsPage } from './pages/organizations/organizations'
import { OrganizationSettingsPage } from './pages/organizations/settings'
import { OverviewPage } from './pages/overview'
import { ClusterDetailPage } from './pages/servers/cluster-detail'
import { ServersPage } from './pages/servers/servers'
import { UserDetailPage } from './pages/users/user-detail'
import { UsersPage } from './pages/users/users'

function Console() {
  return (
    <RequireAuth>
      <OrganizationsProvider>
        <ConsoleLayout>
          <Outlet />
        </ConsoleLayout>
      </OrganizationsProvider>
    </RequireAuth>
  )
}

// /clusters/:ref moved under /servers; old links still work.
function OldClusterRoute() {
  const { ref = '' } = useParams()
  return <Navigate to={`/servers/clusters/${encodeURIComponent(ref)}`} replace />
}

function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />

      <Route element={<DocsLayout />}>
        <Route path="/docs" element={<DocsPage />} />
        <Route path="/docs/*" element={<DocsPage />} />
      </Route>

      <Route element={<Console />}>
        <Route path="/" element={<OverviewPage />} />
        <Route path="/servers" element={<ServersPage />} />
        <Route path="/servers/clusters/:ref" element={<ClusterDetailPage />} />
        <Route path="/clusters" element={<Navigate to="/servers" replace />} />
        <Route path="/clusters/:ref" element={<OldClusterRoute />} />
        <Route path="/nodes" element={<Navigate to="/servers?tab=nodes" replace />} />

        <Route path="/organizations" element={<OrganizationsPage />} />
        <Route path="/organizations/:org" element={<OrganizationRoute />}>
          <Route index element={<Navigate to="members" replace />} />
          <Route path="members" element={<MembersPage />} />
          <Route path="api-keys" element={<ApiKeysPage />} />
          <Route path="settings" element={<OrganizationSettingsPage />} />
        </Route>

        <Route
          path="/users"
          element={
            <RequireAdmin>
              <UsersPage />
            </RequireAdmin>
          }
        />
        <Route
          path="/users/:ref"
          element={
            <RequireAdmin>
              <UserDetailPage />
            </RequireAdmin>
          }
        />
        <Route path="/account" element={<AccountPage />} />
      </Route>

      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  )
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <AppLinkProvider>
        <AuthProvider>
          <App />
        </AuthProvider>
      </AppLinkProvider>
    </BrowserRouter>
  </StrictMode>,
)
