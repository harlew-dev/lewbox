import {
  Button,
  ErrorNotice,
  Field,
  Input,
  Label,
  PageHeader,
  PageTabs,
  tabFrom,
  toast,
  useAction,
} from '@harlew-dev/ui'
import { useState, type FormEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import * as api from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { useDocumentTitle } from '../../lib/use-document-title'
import { failureMessage } from '../../lib/use-load'
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
    <Button onClick={() => setOpen(true)}>Create management key</Button>
  )

  return (
    <>
      <PageHeader title="Team" actions={action} />
      {can.manage && <WorkspaceName key={org.name} />}
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

// Renames the workspace, for those who may manage it.
function WorkspaceName() {
  const { org, reload } = usePortalOrg()
  const { reloadOrganizations } = useAuth()
  const [name, setName] = useState(org.name)
  const clean = name.trim()
  const rename = useAction(() => api.updateOrganization(org.slug, { name: clean }), {
    toError: (failure) => ({ message: failureMessage(failure) }),
  })

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await rename.run()
    if (result.ok) {
      toast.success('Workspace renamed.')
      reload()
      void reloadOrganizations()
    }
  }

  return (
    <form className="mb-10 max-w-xl space-y-4" onSubmit={submit}>
      <div className="flex items-end gap-3">
        <Field disabled={rename.busy} className="flex-1">
          <Label>Workspace name</Label>
          <Input value={name} onChange={(event) => setName(event.currentTarget.value)} />
        </Field>
        <Button
          type="submit"
          outline
          loading={rename.loading}
          disabled={clean === '' || clean === org.name}
          disabledReason={clean === '' ? 'Fill in required fields' : 'No changes made'}
        >
          Rename
        </Button>
      </div>
      <ErrorNotice error={rename.error} />
    </form>
  )
}
