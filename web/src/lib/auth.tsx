import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from 'react'
import { Navigate, useLocation } from 'react-router'
import {
  Button,
  StatusPage,
  TextLink,
} from '@harlew-dev/ui'
import * as api from './api'
import type { MeOrganization, User } from './api'
import { NotFoundPage } from '../pages/not-found'
import { Spinner } from '../components/loading'

type Status = 'loading' | 'authenticated' | 'guest' | 'error'

interface AuthContextValue {
  user: User | null
  // The organizations the account belongs to, from /v0/me.
  organizations: MeOrganization[]
  status: Status
  error: string
  signIn: (email: string, password: string) => Promise<void>
  signUp: (input: { email: string; password: string; setup_token?: string }) => Promise<void>
  signOut: () => Promise<void>
  refresh: () => Promise<void>
  // Reloads /v0/me without the loading state, after joining or leaving.
  reloadOrganizations: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [organizations, setOrganizations] = useState<MeOrganization[]>([])
  const [status, setStatus] = useState<Status>('loading')
  const [error, setError] = useState('')

  const refresh = useCallback(async () => {
    setStatus('loading')
    setError('')
    try {
      const result = await api.me()
      if (result.user) {
        setUser(result.user)
        setOrganizations(result.organizations ?? [])
        setStatus('authenticated')
      } else {
        setUser(null)
        setStatus('guest')
      }
    } catch (failure) {
      if (failure instanceof api.ApiError && failure.status === 401) {
        setUser(null)
        setStatus('guest')
        return
      }
      setUser(null)
      setStatus('error')
      setError(failure instanceof Error ? failure.message : 'Could not load your session.')
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  // A request made while signed in that answers 401 means the session was
  // revoked or expired. Drop it; RequireAuth sends the browser to sign in.
  useEffect(() => {
    return api.onUnauthorized(() => {
      setUser(null)
      setStatus('guest')
    })
  }, [])

  const reloadOrganizations = useCallback(async () => {
    try {
      const result = await api.me()
      setOrganizations(result.organizations ?? [])
    } catch {
      // The next full load picks them up.
    }
  }, [])

  const signedIn = useCallback(
    async (user: User) => {
      await reloadOrganizations()
      setUser(user)
      setStatus('authenticated')
      setError('')
    },
    [reloadOrganizations],
  )

  const signIn = useCallback(
    async (email: string, password: string) => {
      const result = await api.signIn(email, password)
      await signedIn(result.user)
    },
    [signedIn],
  )

  const signUp = useCallback(
    async (input: { email: string; password: string; setup_token?: string }) => {
      const result = await api.signUp(input)
      await signedIn(result.user)
    },
    [signedIn],
  )

  const signOut = useCallback(async () => {
    try {
      await api.signOut()
    } finally {
      setUser(null)
      setOrganizations([])
      setStatus('guest')
      setError('')
    }
  }, [])

  return (
    <AuthContext.Provider
      value={{ user, organizations, status, error, signIn, signUp, signOut, refresh, reloadOrganizations }}
    >
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext)
  if (!value) throw new Error('useAuth must be used inside AuthProvider')
  return value
}

export function RequireAuth({ children }: { children: ReactNode }) {
  const { status, error, refresh } = useAuth()
  const location = useLocation()

  if (status === 'loading') {
    return (
      <div className="flex min-h-dvh items-center justify-center">
        <Spinner label="Loading your session" />
      </div>
    )
  }
  if (status === 'error') {
    return (
      <StatusPage
        title="Could not load the page"
        description={error || 'The Lewbox server did not answer.'}
        action={
          <Button onClick={() => void refresh()}>
            Try again
          </Button>
        }
        logo={
          <TextLink href="/" className="text-sm">
            Lewbox
          </TextLink>
        }
      />
    )
  }
  if (status === 'guest') {
    const next = `${location.pathname}${location.search}`
    return <Navigate to={`/login?next=${encodeURIComponent(next)}`} replace />
  }
  return <>{children}</>
}

// The console is invisible to everyone else: they get the not-found page.
export function RequireAdmin({ children }: { children: ReactNode }) {
  const { user } = useAuth()
  if (!user?.instance_admin) return <NotFoundPage />
  return <>{children}</>
}

// Where an account lands after signing in: the console for instance admins,
// the portal for everyone else.
export function homeFor(user: User | null): string {
  return user?.instance_admin ? '/admin' : '/'
}
