import {
  Button,
  ConfirmationDialog,
  DangerAction,
  DescriptionDetails,
  DescriptionList,
  DescriptionTerm,
  Divider,
  ErrorNotice,
  Field,
  Input,
  Label,
  PageHeader,
  SettingsSection,
  toast,
  useAction,
} from '@harlew-dev/ui'
import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router'
import * as api from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { formatLimit, formatMemory } from '../../lib/format'
import { useDocumentTitle } from '../../lib/use-document-title'
import { failureMessage } from '../../lib/use-load'
import { usePortalOrg } from './portal-route'

export function SettingsPage() {
  useDocumentTitle('Settings')
  const { org, can, reload } = usePortalOrg()
  const { user, reloadOrganizations } = useAuth()
  const navigate = useNavigate()
  const [name, setName] = useState(org.name)
  const [leaveOpen, setLeaveOpen] = useState(false)
  const clean = name.trim()

  const rename = useAction(() => api.updateOrganization(org.slug, { name: clean }), {
    toError: (failure) => ({ message: failureMessage(failure) }),
  })

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await rename.run()
    if (result.ok) {
      toast.success('Organization renamed.')
      reload()
      void reloadOrganizations()
    }
  }

  return (
    <div className="max-w-4xl">
      <PageHeader title="Settings" />
      <Divider className="mb-10" />

      <SettingsSection title="Organization name">
        <form className="space-y-4" onSubmit={submit}>
          <Field disabled={!can.manage || rename.busy}>
            <Label className="sr-only">Organization name</Label>
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

      <SettingsSection title="Plan" description="Limits hold across every region together.">
        <DescriptionList>
          <DescriptionTerm>Plan</DescriptionTerm>
          <DescriptionDetails>{org.plan?.name ?? 'None'}</DescriptionDetails>
          <DescriptionTerm>Sandboxes at once</DescriptionTerm>
          <DescriptionDetails>{formatLimit(org.limits.sandboxes)}</DescriptionDetails>
          <DescriptionTerm>vCPUs</DescriptionTerm>
          <DescriptionDetails>{formatLimit(org.limits.vcpus)}</DescriptionDetails>
          <DescriptionTerm>Memory</DescriptionTerm>
          <DescriptionDetails>{formatLimit(org.limits.memory_mb, formatMemory)}</DescriptionDetails>
        </DescriptionList>
      </SettingsSection>

      <Divider soft className="my-10" />

      <SettingsSection title="Leave" variant="danger">
        <DangerAction
          title="Leave organization"
          description="You lose access until someone adds you again. An organization keeps at least one owner."
          action={
            <Button color="red" onClick={() => setLeaveOpen(true)}>
              Leave
            </Button>
          }
        />
      </SettingsSection>

      <ConfirmationDialog
        open={leaveOpen}
        onClose={() => setLeaveOpen(false)}
        title={`Leave ${org.name}?`}
        description="You lose access to its sandboxes, keys and usage until someone adds you again."
        confirm="Leave organization"
        variant="danger"
        onConfirm={async () => {
          const members = await api.listMembers(org.slug)
          const self = members.data.find((member) => member.user_id === user?.id)
          if (!self) throw new Error('You are not a member of this organization.')
          await api.removeMember(org.slug, self.id)
        }}
        onConfirmed={async () => {
          toast.success(`You left ${org.name}.`)
          await reloadOrganizations()
          navigate('/')
        }}
      />
    </div>
  )
}
