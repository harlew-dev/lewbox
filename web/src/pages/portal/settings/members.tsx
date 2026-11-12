import { Button } from '@harlew-dev/ui'
import { useState } from 'react'
import { useNavigate } from 'react-router'
import { useAuth } from '../../../lib/auth'
import { useDocumentTitle } from '../../../lib/use-document-title'
import { MembersSection } from '../../organizations/members'
import { usePortalOrg } from '../portal-route'

export function SettingsMembersPage() {
  useDocumentTitle('Members')
  const { org, can } = usePortalOrg()
  const { reloadOrganizations } = useAuth()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)

  return (
    <>
      <div className="mb-6 flex justify-end">
        {can.manage && <Button onClick={() => setOpen(true)}>Add member</Button>}
      </div>
      <MembersSection
        org={org}
        can={can}
        addOpen={open}
        onAddClose={() => setOpen(false)}
        onLeft={() => {
          void reloadOrganizations()
          navigate('/')
        }}
      />
    </>
  )
}
