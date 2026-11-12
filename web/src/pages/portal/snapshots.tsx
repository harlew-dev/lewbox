import { EmptyState, PageHeader } from '@harlew-dev/ui'
import { Snapshots } from '../../components/snapshot-table'
import { useDocumentTitle } from '../../lib/use-document-title'
import { usePortalOrg } from './portal-route'

export function SnapshotsPage() {
  useDocumentTitle('Snapshots')
  const { org, can } = usePortalOrg()
  return (
    <>
      <PageHeader title="Snapshots" description="Every snapshot in every region." />
      <Snapshots
        org={org.slug}
        canDelete={can.sandboxes}
        empty={<EmptyState title="No snapshots" description="Snapshots you take with the SDK show up here." />}
      />
    </>
  )
}
