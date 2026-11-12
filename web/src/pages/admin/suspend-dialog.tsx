import { ConfirmationDialog, toast } from '@harlew-dev/ui'
import * as api from '../../lib/api'

export function SuspendDialog({
  org,
  onClose,
  onSuspended,
}: {
  org: api.Organization | null
  onClose: () => void
  onSuspended: () => void
}) {
  return (
    <ConfirmationDialog
      open={org !== null}
      onClose={onClose}
      title={org ? `Suspend ${org.name}?` : 'Suspend workspace?'}
      description="Its sandboxes are stopped and its API keys stop working until you restore it. Nothing is deleted."
      confirm="Suspend workspace"
      variant="danger"
      onConfirm={async () => {
        if (org) await api.updateOrganization(org.slug, { status: 'suspended' })
      }}
      onConfirmed={() => {
        toast.success('Workspace suspended.')
        onSuspended()
      }}
    />
  )
}
