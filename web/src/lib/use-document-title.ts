import { useEffect } from 'react'

export function useDocumentTitle(title: string) {
  useEffect(() => {
    document.title = title === '' ? 'Lewbox' : `${title} · Lewbox`
    return () => {
      document.title = 'Lewbox'
    }
  }, [title])
}
