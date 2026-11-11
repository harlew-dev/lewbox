import {
  Button,
  DangerAction,
  DescriptionDetails,
  DescriptionList,
  DescriptionTerm,
  Divider,
  ErrorNotice,
  Field,
  Input,
  Label,
  Notice,
  PageHeader,
  SettingsSection,
  toast,
  useAction,
} from '@harlew-dev/ui'
import { useState, type FormEvent } from 'react'
import * as api from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { formatDate } from '../../lib/format'
import { useDocumentTitle } from '../../lib/use-document-title'
import { failureMessage } from '../../lib/use-load'
import { SuspendOrganizationDialog } from './organizations'
import { useOrganizationPage } from './organization-route'
import { OrgStatusBadge, RoleBadge } from './roles'

export function OrganizationSettingsPage() {
  const { org, can, reload } = useOrganizationPage()
  useDocumentTitle(`Settings · ${org.name}`)
  const { user } = useAuth()
  const isAdmin = user?.instance_admin === true
  const [name, setName] = useState(org.name)
  const [suspendOpen, setSuspendOpen] = useState(false)
  const clean = name.trim()

  const rename = useAction(() => api.updateOrganization(org.slug, { name: clean }), {
    toError: (failure) => ({ message: failureMessage(failure) }),
  })
  const restore = useAction(() => api.updateOrganization(org.slug, { status: 'active' }), {
    onError: (failure) => {
      toast.error(failureMessage(failure))
      return true
    },
  })

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await rename.run()
    if (result.ok) {
      toast.success('Organization renamed.')
      reload()
    }
  }

  return (
    <div className="max-w-4xl">
      <PageHeader title="Settings" description={org.name} meta={<OrgStatusBadge status={org.status} />} />
      <Divider className="mb-10" />

      {org.status === 'suspended' && (
        <Notice variant="warning" className="mb-10">
          This organization is suspended. Its API keys don't work and only instance admins can
          change it.
        </Notice>
      )}

      <SettingsSection title="Name" description="Shown in the console. The slug stays the same.">
        <form className="space-y-4" onSubmit={submit}>
          <Field disabled={!can.manage || rename.busy}>
            <Label className="sr-only">Name</Label>
            <Input value={name} onChange={(event) => setName(event.currentTarget.value)} />
          </Field>
          {can.manage && (
            <div className="flex justify-end">
              <Button
                type="submit"
                loading={rename.loading}
                disabled={clean === '' || clean === org.name}
                disabledReason={clean === '' ? 'Fill in required fields' : 'No changes made'}
              >
                Save
              </Button>
            </div>
          )}
          <ErrorNotice error={rename.error} />
        </form>
      </SettingsSection>

      <Divider soft className="my-10" />

      <SettingsSection title="Details">
        <DescriptionList>
          <DescriptionTerm>Slug</DescriptionTerm>
          <DescriptionDetails>{org.slug}</DescriptionDetails>
          <DescriptionTerm>ID</DescriptionTerm>
          <DescriptionDetails className="font-mono text-sm">{org.id}</DescriptionDetails>
          <DescriptionTerm>Members</DescriptionTerm>
          <DescriptionDetails>{org.members}</DescriptionDetails>
          <DescriptionTerm>Your role</DescriptionTerm>
          <DescriptionDetails>{org.role ? <RoleBadge role={org.role} /> : 'Instance admin'}</DescriptionDetails>
          <DescriptionTerm>Created</DescriptionTerm>
          <DescriptionDetails>{formatDate(org.created_at)}</DescriptionDetails>
        </DescriptionList>
      </SettingsSection>

      {isAdmin && (
        <>
          <Divider soft className="my-10" />
          {org.status === 'suspended' ? (
            <SettingsSection title="Suspended" description="Restoring it makes its API keys work again." stacked>
              <div className="flex justify-end">
                <Button
                  outline
                  loading={restore.loading}
                  onClick={async () => {
                    const result = await restore.run()
                    if (result.ok) {
                      toast.success(`${org.name} restored.`)
                      reload()
                    }
                  }}
                >
                  Restore organization
                </Button>
              </div>
            </SettingsSection>
          ) : (
            <SettingsSection title="Danger zone" variant="danger">
              <DangerAction
                title="Suspend organization"
                description="Its API keys stop working and its members can only look, until an instance admin restores it."
                action={
                  <Button color="red" onClick={() => setSuspendOpen(true)}>
                    Suspend
                  </Button>
                }
              />
            </SettingsSection>
          )}
        </>
      )}

      <SuspendOrganizationDialog
        org={suspendOpen ? org : null}
        onClose={() => setSuspendOpen(false)}
        onSuspended={reload}
      />
    </div>
  )
}
