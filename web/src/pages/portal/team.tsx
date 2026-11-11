import { Button, PageHeader, PageTabs, tabFrom } from '@harlew-dev/ui'
import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { useAuth } from '../../lib/auth'
import { useDocumentTitle } from '../../lib/use-document-title'
import { AdminKeysSection } from '../organizations/api-keys'
import { MembersSection } from '../organizations/members'
import { usePortalOrg } from './portal-route'

export function TeamPage() {
  useDocumentTitle('Team')
  const { org, can } = usePortalOrg()
  const { reloadOrganizations } = useAuth()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const tabs = can.keys
    ? ([
        { id: 'members', label: 'Members' },
        { id: 'keys', label: 'Management API keys' },
      ] as const)
    : ([{ id: 'members', label: 'Members' }] as const)
  const tab = tabFrom(searchParams, tabs)
  const [open, setOpen] = useState(false)

  const action = !can.manage ? undefined : tab === 'members' ? (
    <Button onClick={() => setOpen(true)}>Add member</Button>
  ) : (
    <Button onClick={() => setOpen(true)}>Create API key</Button>
  )

  return (
    <>
      <PageHeader title="Team" actions={action} />
      {tabs.length > 1 && <PageTabs label="Team views" tabs={tabs} current={tab} className="mb-8" />}
      <div>
        {tab === 'members' ? (
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
        ) : (
          <AdminKeysSection org={org} can={can} createOpen={open} onCreateClose={() => setOpen(false)} />
        )}
      </div>
    </>
  )
}
