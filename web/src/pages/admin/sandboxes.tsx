import { EmptyState, ErrorNotice, ListSkeleton, PageHeader } from '@harlew-dev/ui'
import { useState } from 'react'
import * as api from '../../lib/api'
import { SandboxFilters } from '../../components/sandbox-filters'
import { SandboxTable } from '../../components/sandbox-table'
import { useRegions } from '../../lib/regions'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'

// Running first, the longest-running at the top: what to look at first
// when something is being abused.
function byLongestRunning(a: api.Sandbox, b: api.Sandbox): number {
  const running = Number(b.status === 'started') - Number(a.status === 'started')
  return running || a.created_at.localeCompare(b.created_at)
}

export function FleetSandboxesPage() {
  useDocumentTitle('Sandboxes')
  const [region, setRegion] = useState('')
  const [status, setStatus] = useState('')
  const sandboxes = useLoad(() => api.listSandboxes({ region, status }), [region, status], { every: 10_000 })
  const list = [...(sandboxes.data?.data ?? [])].sort(byLongestRunning)
  const regions = useRegions()

  return (
    <>
      <PageHeader title="Sandboxes" description="Every workspace’s sandboxes, longest running first." />
      <SandboxFilters regions={regions.list} region={region} status={status} onRegion={setRegion} onStatus={setStatus} />
      {sandboxes.loading && !sandboxes.data ? (
        <ListSkeleton label="Loading sandboxes" rows={4} />
      ) : sandboxes.error.message ? (
        <ErrorNotice error={sandboxes.error} onRetry={sandboxes.reload} />
      ) : list.length === 0 ? (
        <EmptyState title="No sandboxes" description="Sandboxes workspaces start show up here within seconds." />
      ) : (
        <SandboxTable sandboxes={list} regionName={regions.name} fleet canChange onChanged={sandboxes.refresh} />
      )}
    </>
  )
}
