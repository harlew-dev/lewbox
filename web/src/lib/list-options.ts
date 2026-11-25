import { useEffect, useRef, useState } from 'react'
import type { Page } from '@harlew-dev/ui'
import type { FilterOption } from '../components/filter-bar'
import * as api from './api'

// Filter options that come from a paged list: the popover searches and loads
// more through these, 20 at a time.
export function loadOrganizationOptions(q: string, page: number): Promise<Page<FilterOption>> {
  return api.listOrganizations({ q, page, per_page: 20, sort: 'name' }).then((answer) => ({
    data: answer.data.map((org) => ({ value: org.slug, label: org.name })),
    pages: answer.pages,
  }))
}

// The audit trail's actor filter accepts an ID or an email; an email is what a
// person recognises, so that is the value.
export function loadUserOptions(q: string, page: number): Promise<Page<FilterOption>> {
  return api.listUsers({ q, page, per_page: 20, sort: 'email' }).then((answer) => ({
    data: answer.data.map((user) => ({ value: user.email, label: user.email })),
    pages: answer.pages,
  }))
}

// Labels for applied values the popover hasn't loaded, so a badge can name a
// workspace or account rather than show its slug or email.
export function useOptionLabels(
  values: string[],
  load: (q: string, page: number) => Promise<Page<FilterOption>>,
): Record<string, string> {
  const [labels, setLabels] = useState<Record<string, string>>({})
  const loader = useRef(load)
  loader.current = load
  const wantedKey = values.filter(Boolean).join('\u0000')

  useEffect(() => {
    const wanted = wantedKey === '' ? [] : wantedKey.split('\u0000')
    const missing = wanted.filter((value) => labels[value] === undefined)
    if (missing.length === 0) return
    let cancelled = false
    void Promise.all(
      missing.map(async (value) => {
        try {
          const answer = await loader.current(value, 1)
          const hit =
            answer.data.find((option) => option.value === value) ??
            (answer.data.length === 1 ? answer.data[0] : undefined)
          return hit ? ([value, hit.label] as const) : null
        } catch {
          return null
        }
      }),
    ).then((pairs) => {
      if (cancelled) return
      const found = Object.fromEntries(pairs.filter((pair): pair is readonly [string, string] => pair !== null))
      if (Object.keys(found).length > 0) setLabels((current) => ({ ...current, ...found }))
    })
    return () => {
      cancelled = true
    }
  }, [wantedKey, labels])

  return labels
}
