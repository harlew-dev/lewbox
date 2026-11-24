import { Button, Code, Notice, TextLink } from '@harlew-dev/ui'
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
      <Notice variant="info" className="mb-6">
        Management API keys (<Code>lba_…</Code>) let your backend call the Lewbox API for this workspace, without
        a person's password. To run sandboxes, use a <TextLink href="/keys">sandbox API key</TextLink> instead.{' '}
        <TextLink href="/docs/reference/management-api">Learn more</TextLink>
      </Notice>
      <div className="mb-6 flex justify-end">
        {can.manage && <Button onClick={() => setOpen(true)}>Create management key</Button>}
      </div>
      <AdminKeysSection org={org} can={can} createOpen={open} onCreateClose={() => setOpen(false)} />
    </>
  )
}
