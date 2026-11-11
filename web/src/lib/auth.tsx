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
  ListSkeleton,
  StatusPage,
  TextLink,
} from '@harlew-dev/ui'
import * as api from './api'
import type { User } from './api'

type Status = 'loading' | 'authenticated' | 'guest' | 'error'

interface AuthContextValue {
  user: User | null
  status: Status
  error: string
  signIn: (email: string, password: string) => Promise<void>
  signOut: () => Promise<void>
  refresh: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [status, setStatus] = useState<Status>('loading')
  const [error, setError] = useState('')

  const refresh = useCallback(async () => {
    setStatus('loading')
    setError('')
    try {
      const result = await api.me()
      if (result.user) {
        setUser(result.user)
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

  const signIn = useCallback(async (email: string, password: string) => {
    const result = await api.signIn(email, password)
    setUser(result.user)
    setStatus('authenticated')
    setError('')
  }, [])

  const signOut = useCallback(async () => {
    try {
      await api.signOut()
    } finally {
      setUser(null)
      setStatus('guest')
      setError('')
    }
  }, [])

  return (
    <AuthContext.Provider value={{ user, status, error, signIn, signOut, refresh }}>
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
      <div className="mx-auto max-w-6xl px-6 py-16">
        <ListSkeleton label="Loading your session" rows={2} />
      </div>
    )
  }
  if (status === 'error') {
    return (
      <StatusPage
        title="Could not load the console"
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

export function RequireAdmin({ children }: { children: ReactNode }) {
  const { user } = useAuth()
  if (!user?.instance_admin) {
    return (
      <StatusPage
        title="Admins only"
        description="This page is for instance admins. Ask an admin if you need access."
        action={<Button href="/">Back to the overview</Button>}
      />
    )
  }
  return <>{children}</>
}

