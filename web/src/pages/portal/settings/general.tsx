import {
  Button,
  ConfirmationDialog,
  CopyButton,
  CopyFailure,
  DangerAction,
  DescriptionList,
  DescriptionDetails,
  DescriptionTerm,
  Divider,
  ErrorNotice,
  Field,
  Input,
  InputActions,
  InputGroup,
  Label,
  SettingsSection,
  Text,
  toast,
  useAction,
  useCopy,
} from '@harlew-dev/ui'
import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router'
import * as api from '../../../lib/api'
import { useAuth } from '../../../lib/auth'
import { useOrganizations } from '../../../lib/organizations'
import { useDocumentTitle } from '../../../lib/use-document-title'
import { failureMessage, useLoad } from '../../../lib/use-load'
import { usePortalOrg } from '../portal-route'

export function SettingsGeneralPage() {
  useDocumentTitle('Workspace settings')
  const { org, can } = usePortalOrg()
  return (
    <>
      <SettingsSection title="General">
        {can.manage ? (
          <RenameForm key={org.slug} />
        ) : (
          <Field locked="Only owners and admins can rename this">
            <Label>Workspace name</Label>
            <Input value={org.name} readOnly />
          </Field>
        )}
      </SettingsSection>

      <Divider soft className="my-10" />
      <SettingsSection title="Region">
        {org.region ? (
          <DescriptionList>
            <DescriptionTerm>Region</DescriptionTerm>
            <DescriptionDetails>{org.region.name}</DescriptionDetails>
            <DescriptionTerm>Plan</DescriptionTerm>
            <DescriptionDetails>{org.region.plan.name}</DescriptionDetails>
          </DescriptionList>
        ) : (
          <Text>No region yet — your provider will assign one.</Text>
        )}
      </SettingsSection>

      <Divider soft className="my-10" />
      <SettingsSection title="Workspace ID">
        <CopyField label="Workspace ID" value={org.slug} lock="Read-only" />
      </SettingsSection>

      <Divider soft className="my-10" />
      <LeaveZone org={org} />
    </>
  )
}

// Renames the workspace, then reloads it and the switcher so the new name
// shows everywhere.
function RenameForm() {
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
    <form onSubmit={submit} className="space-y-4">
      <Field disabled={rename.busy}>
        <Label>Workspace name</Label>
        <Input value={name} onChange={(event) => setName(event.currentTarget.value)} />
      </Field>
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
      <ErrorNotice error={rename.error} />
    </form>
  )
}

// A read-only value with a Copy button inside the input.
function CopyField({ label, value, lock }: { label: string; value: string; lock: string }) {
  const copy = useCopy(value)
  return (
    <div className="space-y-2">
      <Field locked={lock}>
        <Label className="sr-only">{label}</Label>
        <InputGroup>
          <Input
            value={value}
            className="font-mono"
            readOnly
            onFocus={(event) => event.currentTarget.select()}
          />
          <InputActions>
            <CopyButton copy={copy} label={`Copy ${label}`} />
          </InputActions>
        </InputGroup>
      </Field>
      <CopyFailure copy={copy}>Couldn’t copy. Select the ID and copy it instead.</CopyFailure>
    </div>
  )
}

// Leaving takes the member out at once. The only owner doesn't see it: they
// can't leave.
function LeaveZone({ org }: { org: api.Organization }) {
  const { user } = useAuth()
  const { organizations, select } = useOrganizations()
  const { reloadOrganizations } = useAuth()
  const navigate = useNavigate()
  const members = useLoad(() => api.listMembers(org.slug), [org.slug])
  const [open, setOpen] = useState(false)
  const list = members.data?.data ?? []
  const self = list.find((member) => member.user_id === user?.id)
  const owners = list.filter((member) => member.role === 'owner')
  const canLeave = self !== undefined && !(self.role === 'owner' && owners.length === 1)
  if (!canLeave) return null

  async function leave() {
    if (!self) return
    await api.removeMember(org.slug, self.id)
    await reloadOrganizations()
    const next = organizations.find((other) => other.slug !== org.slug)
    if (next) select(next.slug)
    navigate('/')
  }

  return (
    <>
      <SettingsSection title="Danger zone" variant="danger">
        <DangerAction
          title="Leave workspace"
          description="You lose access to this workspace and its sandboxes."
          action={
            <Button color="red" onClick={() => setOpen(true)}>
              Leave workspace
            </Button>
          }
        />
      </SettingsSection>
      <ConfirmationDialog
        open={open}
        onClose={() => setOpen(false)}
        title={`Leave ${org.name}?`}
        description="You lose access at once. To come back, someone will need to add you again."
        confirm="Leave workspace"
        variant="danger"
        onConfirm={leave}
        onConfirmed={() => {
          setOpen(false)
          toast.success(`You left ${org.name}.`)
        }}
      />
    </>
  )
}
