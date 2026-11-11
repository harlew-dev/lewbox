import { CheckboxField, ConfirmationDialog, toast } from '@harlew-dev/ui'
import { useState } from 'react'
import * as api from '../../lib/api'

// Confirms removing a node: lewbox node remove [--force].
export function RemoveNodeDialog({
  node,
  onClose,
  onRemoved,
}: {
  node: api.Node | null
  onClose: () => void
  onRemoved: () => void
}) {
  const [force, setForce] = useState(false)
  const [shownFor, setShownFor] = useState(node)
  if (node !== shownFor) {
    setShownFor(node)
    if (node) setForce(false)
  }
  return (
    <ConfirmationDialog
      open={node !== null}
      onClose={onClose}
      title={node ? `Remove ${node.name}?` : 'Remove node?'}
      description="Lewbox drains the node, removes it from its AerolVM cluster and forgets it. AerolVM stays installed on the machine. Sandboxes still on the node block removal unless you force it."
      confirm="Remove node"
      variant="danger"
      onConfirm={async () => {
        if (node) await api.removeNode(node.name, force)
      }}
      onConfirmed={() => {
        toast.success('Node removed.')
        onRemoved()
      }}
    >
      <CheckboxField checked={force} onChange={setForce}>
        Force removal, even with sandboxes on it
      </CheckboxField>
    </ConfirmationDialog>
  )
}
