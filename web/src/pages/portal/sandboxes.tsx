import { Button, EmptyState, ErrorNotice, ListSkeleton, PageHeader } from '@harlew-dev/ui'
import { useState } from 'react'
import * as api from '../../lib/api'
import { SandboxFilters } from '../../components/sandbox-filters'
import { SandboxTable } from '../../components/sandbox-table'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'
import { usePortalOrg } from './portal-route'

export function SandboxesPage() {
  useDocumentTitle('Sandboxes')
  const { org, can } = usePortalOrg()
  const [region, setRegion] = useState('')
  const [status, setStatus] = useState('')
  const sandboxes = useLoad(() => api.listSandboxes({ org: org.slug, region, status }), [org.slug, region, status], {
    every: 10_000,
  })
  const list = sandboxes.data?.data ?? []
  const filtered = region !== '' || status !== ''

  return (
    <>
      <PageHeader title="Sandboxes" description="Every sandbox in every region. Refreshes every 10 seconds." />
      <SandboxFilters region={region} status={status} onRegion={setRegion} onStatus={setStatus} />

      {sandboxes.loading && !sandboxes.data ? (
        <ListSkeleton label="Loading sandboxes" rows={4} />
      ) : sandboxes.error.message ? (
        <ErrorNotice error={sandboxes.error} onRetry={sandboxes.reload} />
      ) : list.length === 0 ? (
        filtered ? (
          <EmptyState title="No sandboxes match" description="Try another region or status." />
        ) : (
          <EmptyState
            title="No sandboxes"
            description="Sandboxes you start with the SDK show up here."
            action={<Button href="/keys">Get an API key</Button>}
          />
        )
      ) : (
        <SandboxTable sandboxes={list} canChange={can.sandboxes} org={org.slug} onChanged={sandboxes.refresh} />
      )}
    </>
  )
}
