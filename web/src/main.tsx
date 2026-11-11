import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router'
import './styles/app.css'
import { App } from './app'
import { AppLinkProvider } from './components/app-link'
import { AuthProvider } from './lib/auth'
import { InstanceProvider } from './lib/instance'
import { OrganizationsProvider } from './lib/organizations'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <AppLinkProvider>
        <InstanceProvider>
          <AuthProvider>
            <OrganizationsProvider>
              <App />
            </OrganizationsProvider>
          </AuthProvider>
        </InstanceProvider>
      </AppLinkProvider>
    </BrowserRouter>
  </StrictMode>,
)
