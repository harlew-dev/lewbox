import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import * as api from './api'

interface InstanceValue extends Required<api.Instance> {
  loaded: boolean
  reload: () => void
}

const fallback: Required<api.Instance> = { name: 'Lewbox', signup: 'closed', billing: 'off', currency: '' }

const InstanceContext = createContext<InstanceValue>({ ...fallback, loaded: false, reload: () => {} })

// The instance's public settings (name, sign-up, billing), loaded once. If
// they can't be loaded the app behaves as a closed instance without billing.
export function InstanceProvider({ children }: { children: ReactNode }) {
  const [instance, setInstance] = useState<Required<api.Instance>>(fallback)
  const [loaded, setLoaded] = useState(false)
  const [tick, setTick] = useState(0)

  useEffect(() => {
    let cancelled = false
    api
      .instance()
      .then((value) => {
        if (!cancelled) setInstance({ ...fallback, ...value })
      })
      .catch(() => {})
      .finally(() => {
        if (!cancelled) setLoaded(true)
      })
    return () => {
      cancelled = true
    }
  }, [tick])

  const reload = useCallback(() => setTick((value) => value + 1), [])
  const value = useMemo(() => ({ ...instance, loaded, reload }), [instance, loaded, reload])
  return <InstanceContext.Provider value={value}>{children}</InstanceContext.Provider>
}

export function useInstance(): InstanceValue {
  return useContext(InstanceContext)
}
