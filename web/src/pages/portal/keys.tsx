import { Button, EmptyState, ErrorNotice, ListSkeleton, PageHeader } from '@harlew-dev/ui'
import { useState } from 'react'
import * as api from '../../lib/api'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'
import { noRegionText } from '../../components/no-region'
import { SandboxKeyTable } from '../../components/sandbox-key-table'
import { CreateKeyDialog } from './create-key-dialog'
import { usePortalOrg } from './portal-route'

export function KeysPage() {
  useDocumentTitle('API keys')
  const { org, can } = usePortalOrg()
  const [createOpen, setCreateOpen] = useState(false)
  const keys = useLoad(() => api.listSandboxKeys(org.slug), [org.slug])
  const list = keys.data?.data ?? []
  const noRegion = org.regions.length === 0
  const create = can.use ? (
    <Button onClick={() => setCreateOpen(true)} disabled={noRegion} disabledReason={noRegionText}>
      Create API key
    </Button>
  ) : undefined

  return (
    <>
      <PageHeader
        title="API keys"
        description="Each key works in one region, with the AerolVM or E2B SDK."
        actions={list.length > 0 ? create : undefined}
      />

      {keys.loading && !keys.data ? (
        <ListSkeleton label="Loading API keys" rows={3} />
      ) : keys.error.message ? (
        <ErrorNotice error={keys.error} onRetry={keys.reload} />
      ) : list.length === 0 ? (
        <EmptyState
          title="No API keys"
          description={noRegion ? noRegionText : 'Create a key for a region, then run your first sandbox with it.'}
          action={create}
        />
      ) : (
        <SandboxKeyTable keys={list} org={org.slug} canRevoke={can.use} onRevoked={keys.refresh} />
      )}

      <CreateKeyDialog open={createOpen} org={org} onClose={() => setCreateOpen(false)} onCreated={keys.refresh} />
    </>
  )
}
