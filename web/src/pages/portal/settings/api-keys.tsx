import { Button } from '@harlew-dev/ui'
import { useState } from 'react'
import { useDocumentTitle } from '../../../lib/use-document-title'
import { AdminKeysSection } from '../../organizations/api-keys'
import { usePortalOrg } from '../portal-route'

export function SettingsKeysPage() {
  useDocumentTitle('API keys')
  const { org, can } = usePortalOrg()
  const [open, setOpen] = useState(false)

  return (
    <>
      <div className="mb-6 flex justify-end">
        {can.manage && <Button onClick={() => setOpen(true)}>Create management key</Button>}
      </div>
      <AdminKeysSection org={org} can={can} createOpen={open} onCreateClose={() => setOpen(false)} />
    </>
  )
}
