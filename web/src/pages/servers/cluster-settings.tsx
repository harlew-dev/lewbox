import {
  Button,
  Code,
  DescriptionDetails,
  DescriptionList,
  DescriptionTerm,
  Description,
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
  Divider,
  ErrorMessage,
  ErrorNotice,
  Field,
  Input,
  Label,
  Listbox,
  ListboxOption,
  RadioGroup,
  Subheading,
  TextLink,
  toast,
  useAction,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { useState } from 'react'
import * as api from '../../lib/api'
import { validDomain } from '../../lib/commands'
import { failureMessage, useLoad } from '../../lib/use-load'

export const stopPolicyLabel = (policy: string) =>
  policy === 'archive' ? 'Archive it to storage' : 'Keep it on its server'

// The cluster's DNS and storage settings, with an Edit button.
export function ClusterSettings({
  cluster,
  onEdit,
}: {
  cluster: api.Cluster
  onEdit: () => void
}) {
  return (
    <>
      <div className="mt-12 flex items-center justify-between">
        <Subheading>Settings</Subheading>
        <Button outline onClick={onEdit}>
          Edit
        </Button>
      </div>
      <Divider className="mt-4" />
      <DescriptionList>
        <DescriptionTerm>Domain</DescriptionTerm>
        <DescriptionDetails>{cluster.domain ? <Code>{cluster.domain}</Code> : 'None'}</DescriptionDetails>
        <DescriptionTerm>DNS profile</DescriptionTerm>
        <DescriptionDetails>{cluster.dns_profile ?? 'None'}</DescriptionDetails>
        <DescriptionTerm>DNS records</DescriptionTerm>
        <DescriptionDetails>
          {cluster.dns_records === 'off' ? 'Off: you keep them' : 'Lewbox keeps them'}
        </DescriptionDetails>
        <DescriptionTerm>Storage profile</DescriptionTerm>
        <DescriptionDetails>{cluster.storage_profile ?? 'None'}</DescriptionDetails>
        <DescriptionTerm>When a sandbox stops</DescriptionTerm>
        <DescriptionDetails>{stopPolicyLabel(cluster.stop_policy)}</DescriptionDetails>
      </DescriptionList>
    </>
  )
}

export function ClusterSettingsDialog({
  open,
  cluster,
  onClose,
  onSaved,
}: {
  open: boolean
  cluster: api.Cluster
  onClose: () => void
  // The update that was sent, so the page can say what else must happen.
  onSaved: (changes: api.ClusterUpdate) => void
}) {
  const count = useOpenCount(open)
  return (
    <Dialog open={open} onClose={onClose} size="xl">
      <SettingsForm key={count} cluster={cluster} onClose={onClose} onSaved={onSaved} />
    </Dialog>
  )
}

function SettingsForm({
  cluster,
  onClose,
  onSaved,
}: {
  cluster: api.Cluster
  onClose: () => void
  onSaved: (changes: api.ClusterUpdate) => void
}) {
  const [domain, setDomain] = useState(cluster.domain)
  const [dnsProfile, setDNSProfile] = useState(cluster.dns_profile ?? '')
  const [dnsRecords, setDNSRecords] = useState(cluster.dns_records)
  const [storageProfile, setStorageProfile] = useState(cluster.storage_profile ?? '')
  const [stopPolicy, setStopPolicy] = useState(cluster.stop_policy)
  const dnsProfiles = useLoad(() => api.listDNSProfiles(), [])
  const storageProfiles = useLoad(() => api.listStorageProfiles(), [])

  const blocked =
    storageProfile === ''
      ? 'Choose a storage profile first.'
      : cluster.runtime === 'firecracker'
        ? 'Archiving needs the Docker runtime; this cluster uses Firecracker.'
        : ''
  const policy = blocked ? 'keep' : stopPolicy

  const changes: api.ClusterUpdate = {}
  if (domain !== cluster.domain) changes.domain = domain
  if (dnsProfile !== (cluster.dns_profile ?? '')) changes.dns_profile = dnsProfile || 'none'
  if (dnsRecords !== cluster.dns_records) changes.dns_records = dnsRecords
  if (storageProfile !== (cluster.storage_profile ?? '')) changes.storage_profile = storageProfile || 'none'
  if (policy !== cluster.stop_policy) changes.stop_policy = policy
  const changed = Object.keys(changes).length > 0
  const domainProblem = domain !== '' && !validDomain(domain)

  const save = useAction(() => api.updateCluster(cluster.slug, changes), {
    toError: (failure) => ({ message: failureMessage(failure) }),
  })

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    const result = await save.run()
    if (result.ok) {
      toast.success('Cluster settings saved.')
      onSaved(changes)
      onClose()
    }
  }

  return (
    <form onSubmit={submit}>
      <DialogTitle>Edit {cluster.name}’s settings</DialogTitle>
      <DialogDescription>
        Only what you change is sent. Nodes pick up a changed profile when <code>lewbox node configure</code> runs on
        them.
      </DialogDescription>
      <DialogBody className="space-y-6">
        <Field disabled={save.busy}>
          <Label>Domain</Label>
          <Input
            value={domain}
            placeholder="eu.api.example.com"
            invalid={domainProblem}
            onChange={(event) => setDomain(event.currentTarget.value.trim())}
          />
          {domainProblem ? (
            <ErrorMessage>A domain name. Empty clears it.</ErrorMessage>
          ) : (
            <Description>Empty clears the domain. A DNS profile needs a domain in its zone.</Description>
          )}
        </Field>
        <Field disabled={save.busy}>
          <Label>DNS profile</Label>
          <Listbox value={dnsProfile} onChange={setDNSProfile}>
            <ListboxOption value="">None</ListboxOption>
            {(dnsProfiles.data?.data ?? []).map((profile) => (
              <ListboxOption key={profile.id} value={profile.name}>
                {profile.name} ({profile.zone})
              </ListboxOption>
            ))}
          </Listbox>
          <Description>
            <TextLink href="/admin/settings?tab=dns">Settings → DNS profiles</TextLink>
          </Description>
        </Field>
        <RadioGroup
          label="DNS records"
          value={dnsRecords}
          onChange={setDNSRecords}
          disabled={save.busy}
          options={[
            { value: 'auto', label: 'Lewbox keeps them', description: 'Records follow the cluster’s nodes.' },
            { value: 'off', label: 'I keep them', description: 'For a domain pointed at a load balancer.' },
          ]}
        />
        <Field disabled={save.busy}>
          <Label>Storage profile</Label>
          <Listbox value={storageProfile} onChange={setStorageProfile}>
            <ListboxOption value="">None</ListboxOption>
            {(storageProfiles.data?.data ?? []).map((profile) => (
              <ListboxOption key={profile.id} value={profile.name}>
                {profile.name} ({profile.bucket})
              </ListboxOption>
            ))}
          </Listbox>
          <Description>
            <TextLink href="/admin/settings?tab=storage">Settings → Storage profiles</TextLink>
          </Description>
        </Field>
        <RadioGroup
          label="When a sandbox stops"
          value={policy}
          onChange={setStopPolicy}
          disabled={save.busy || blocked !== ''}
          options={[
            { value: 'keep', label: 'Keep it on its server' },
            {
              value: 'archive',
              label: 'Archive it to storage',
              description: blocked ? `Not available: ${blocked}` : 'Restoring starts a new sandbox with a new ID.',
            },
          ]}
        />
        <ErrorNotice error={save.error} />
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={save.busy}>
          Cancel
        </Button>
        <Button
          type="submit"
          loading={save.loading}
          disabled={!changed || domainProblem}
          disabledReason={domainProblem ? 'Fix the domain first' : 'No changes made'}
        >
          Save settings
        </Button>
      </DialogActions>
    </form>
  )
}
