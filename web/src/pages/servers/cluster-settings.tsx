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
  Disclosure,
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
import { routes } from '../../lib/routes'
import { validDomain, type AdvancedForm } from '../../lib/commands'
import { errorField, failureMessage, useLoad } from '../../lib/use-load'

export const stopPolicyLabel = (policy: string) =>
  policy === 'archive' ? 'Archive it to storage' : 'Keep it on its server'

export const retentionOptions: { value: api.ArchiveRetention; label: string; description: string }[] = [
  { value: 'instant', label: 'Remove right away', description: 'The default. Restores download from the bucket.' },
  { value: '1h', label: 'Keep for 1 hour', description: 'A restore on that server within the hour is faster.' },
  { value: '1d', label: 'Keep for 1 day', description: 'A restore on that server within a day is faster.' },
  { value: '7d', label: 'Keep for 7 days', description: 'Uses more disk on each server.' },
  {
    value: 'never',
    label: 'Keep forever',
    description: 'No cleanup: servers’ disks fill up until you free them yourself.',
  },
]

export const retentionLabel = (retention: string) =>
  retentionOptions.find((option) => option.value === retention)?.label ?? retention

export const snapshotNameLabel = (style: string) =>
  style === 'name' ? 'Sandbox name and ID (archive-<name>-<id>)' : 'Sandbox ID (archive-<id>)'

// The two archive settings, shared by the cluster settings dialog and the
// start cluster dialog.
export function ArchiveOptions({
  retention,
  onRetention,
  snapshotName,
  onSnapshotName,
  disabled,
}: {
  retention: api.ArchiveRetention
  onRetention: (value: api.ArchiveRetention) => void
  snapshotName: api.ArchiveSnapshotName
  onSnapshotName: (value: api.ArchiveSnapshotName) => void
  disabled?: boolean
}) {
  const chosen = retentionOptions.find((option) => option.value === retention)
  return (
    <>
      <Field disabled={disabled} className="max-w-md">
        <Label>Local copies on servers</Label>
        <Listbox value={retention} onChange={onRetention}>
          {retentionOptions.map((option) => (
            <ListboxOption key={option.value} value={option.value}>
              {option.label}
            </ListboxOption>
          ))}
        </Listbox>
        <Description>
          The bucket always keeps the archive. {chosen?.description ?? ''} Servers apply a change when{' '}
          <code>lewbox node configure</code> runs on them.
        </Description>
      </Field>
      <RadioGroup
        label="Archive snapshot names"
        value={snapshotName}
        onChange={onSnapshotName}
        disabled={disabled}
        options={[
          { value: 'id', label: 'Sandbox ID', description: 'archive-sb-1a2b3c' },
          {
            value: 'name',
            label: 'Sandbox name and ID',
            description: 'archive-my-app-sb-1a2b3c. The ID stays because names repeat.',
          },
        ]}
      />
    </>
  )
}

// Server field errors from a PATCH or POST, so the matching setting shows the
// message. Other fields keep the general error notice.
const advancedFieldNames = new Set<string>([
  'docker_timeout', 'max_sandbox_cpu', 'max_sandbox_memory_mb', 'max_sandbox_disk_gb',
  'idle_stop_minutes', 'destroy_if_idle_for', 'max_age', 'firecracker_vm_slots',
])

export function advancedFieldError(failure: unknown): { field: keyof AdvancedForm; message: string } | null {
  const field = errorField(failure)
  if (!advancedFieldNames.has(field)) return null
  return { field: field as keyof AdvancedForm, message: failureMessage(failure) }
}

const numberText = (value: number | null) => (value === null ? '' : String(value))

export function advancedFormFromCluster(cluster: api.Cluster): AdvancedForm {
  return {
    docker_timeout: cluster.docker_timeout ?? '',
    max_sandbox_cpu: numberText(cluster.max_sandbox_cpu),
    max_sandbox_memory_mb: numberText(cluster.max_sandbox_memory_mb),
    max_sandbox_disk_gb: numberText(cluster.max_sandbox_disk_gb),
    idle_stop_minutes: numberText(cluster.idle_stop_minutes),
    destroy_if_idle_for: cluster.destroy_if_idle_for ?? '',
    max_age: cluster.max_age ?? '',
    firecracker_vm_slots: numberText(cluster.firecracker_vm_slots),
  }
}

const trimmed = (value: string) => value.trim()

// advancedChanges sends only what changed; an emptied field clears it.
export function advancedChanges(form: AdvancedForm, cluster: api.Cluster): api.ClusterUpdate {
  const changes: api.ClusterUpdate = {}
  const text = (field: 'docker_timeout' | 'destroy_if_idle_for' | 'max_age') => {
    const value = trimmed(form[field])
    const current = cluster[field] ?? ''
    if (value !== current) changes[field] = value === '' ? null : value
  }
  const number = (
    field: 'max_sandbox_cpu' | 'max_sandbox_memory_mb' | 'max_sandbox_disk_gb' | 'idle_stop_minutes' | 'firecracker_vm_slots',
  ) => {
    const value = trimmed(form[field])
    const current = cluster[field]
    if (value === '') {
      if (current !== null) changes[field] = null
      return
    }
    const parsed = Number(value)
    if (!Number.isNaN(parsed) && parsed !== current) changes[field] = parsed
  }
  text('docker_timeout')
  number('max_sandbox_cpu')
  number('max_sandbox_memory_mb')
  number('max_sandbox_disk_gb')
  number('idle_stop_minutes')
  text('destroy_if_idle_for')
  text('max_age')
  if (cluster.runtime === 'firecracker') number('firecracker_vm_slots')
  return changes
}

// advancedCreateInput sends only the settings that were filled in; empty
// fields keep AerolVM's default.
export function advancedCreateInput(form: AdvancedForm, runtime: string) {
  const input: {
    docker_timeout?: string
    max_sandbox_cpu?: number
    max_sandbox_memory_mb?: number
    max_sandbox_disk_gb?: number
    idle_stop_minutes?: number
    destroy_if_idle_for?: string
    max_age?: string
    firecracker_vm_slots?: number
  } = {}
  if (trimmed(form.docker_timeout) !== '') input.docker_timeout = trimmed(form.docker_timeout)
  const number = (field: 'max_sandbox_cpu' | 'max_sandbox_memory_mb' | 'max_sandbox_disk_gb' | 'idle_stop_minutes' | 'firecracker_vm_slots') => {
    const value = trimmed(form[field])
    if (value === '') return
    const parsed = Number(value)
    if (!Number.isNaN(parsed)) input[field] = parsed
  }
  number('max_sandbox_cpu')
  number('max_sandbox_memory_mb')
  number('max_sandbox_disk_gb')
  number('idle_stop_minutes')
  if (trimmed(form.destroy_if_idle_for) !== '') input.destroy_if_idle_for = trimmed(form.destroy_if_idle_for)
  if (trimmed(form.max_age) !== '') input.max_age = trimmed(form.max_age)
  if (runtime === 'firecracker') number('firecracker_vm_slots')
  return input
}

// The collapsed Advanced section shared by the register dialog and the
// cluster settings dialog.
export function AdvancedFields({
  form,
  onChange,
  runtime,
  disabled,
  errors,
}: {
  form: AdvancedForm
  onChange: (field: keyof AdvancedForm, value: string) => void
  runtime: string
  disabled?: boolean
  errors?: Partial<Record<keyof AdvancedForm, string>>
}) {
  return (
    <div className="grid gap-6 sm:grid-cols-2">
      <Field disabled={disabled}>
        <Label>Docker call timeout</Label>
        <Input
          value={form.docker_timeout}
          placeholder="15m"
          invalid={errors?.docker_timeout !== undefined}
          onChange={(event) => onChange('docker_timeout', event.currentTarget.value)}
        />
        {errors?.docker_timeout ? (
          <ErrorMessage>{errors.docker_timeout}</ErrorMessage>
        ) : (
          <Description>How long sandboxd waits on Docker calls, 1m–2h. Default 15m.</Description>
        )}
      </Field>
      <Field disabled={disabled}>
        <Label>Maximum CPUs per sandbox</Label>
        <Input
          type="number"
          step="any"
          min="1"
          value={form.max_sandbox_cpu}
          placeholder="No cap"
          invalid={errors?.max_sandbox_cpu !== undefined}
          onChange={(event) => onChange('max_sandbox_cpu', event.currentTarget.value)}
        />
        {errors?.max_sandbox_cpu ? (
          <ErrorMessage>{errors.max_sandbox_cpu}</ErrorMessage>
        ) : (
          <Description>At least 1, AerolVM’s default size. Empty: no cap.</Description>
        )}
      </Field>
      <Field disabled={disabled}>
        <Label>Maximum memory (MB)</Label>
        <Input
          type="number"
          min="1024"
          value={form.max_sandbox_memory_mb}
          placeholder="No cap"
          invalid={errors?.max_sandbox_memory_mb !== undefined}
          onChange={(event) => onChange('max_sandbox_memory_mb', event.currentTarget.value)}
        />
        {errors?.max_sandbox_memory_mb ? (
          <ErrorMessage>{errors.max_sandbox_memory_mb}</ErrorMessage>
        ) : (
          <Description>At least 1024, AerolVM’s default size. Empty: no cap.</Description>
        )}
      </Field>
      <Field disabled={disabled}>
        <Label>Maximum disk (GB)</Label>
        <Input
          type="number"
          min="10"
          value={form.max_sandbox_disk_gb}
          placeholder="No cap"
          invalid={errors?.max_sandbox_disk_gb !== undefined}
          onChange={(event) => onChange('max_sandbox_disk_gb', event.currentTarget.value)}
        />
        {errors?.max_sandbox_disk_gb ? (
          <ErrorMessage>{errors.max_sandbox_disk_gb}</ErrorMessage>
        ) : (
          <Description>At least 10, AerolVM’s default size. Empty: no cap.</Description>
        )}
      </Field>
      <Field disabled={disabled}>
        <Label>Stop after idle minutes</Label>
        <Input
          type="number"
          min="1"
          value={form.idle_stop_minutes}
          placeholder="Off"
          invalid={errors?.idle_stop_minutes !== undefined}
          onChange={(event) => onChange('idle_stop_minutes', event.currentTarget.value)}
        />
        {errors?.idle_stop_minutes ? (
          <ErrorMessage>{errors.idle_stop_minutes}</ErrorMessage>
        ) : (
          <Description>Stops sandboxes that set no timers of their own. Default off.</Description>
        )}
      </Field>
      <Field disabled={disabled}>
        <Label>Delete after idle</Label>
        <Input
          value={form.destroy_if_idle_for}
          placeholder="Off"
          invalid={errors?.destroy_if_idle_for !== undefined}
          onChange={(event) => onChange('destroy_if_idle_for', event.currentTarget.value)}
        />
        {errors?.destroy_if_idle_for ? (
          <ErrorMessage>{errors.destroy_if_idle_for}</ErrorMessage>
        ) : (
          <Description>A duration such as 24h, at most 720h. Default off.</Description>
        )}
      </Field>
      <Field disabled={disabled}>
        <Label>Maximum age</Label>
        <Input
          value={form.max_age}
          placeholder="Off"
          invalid={errors?.max_age !== undefined}
          onChange={(event) => onChange('max_age', event.currentTarget.value)}
        />
        {errors?.max_age ? (
          <ErrorMessage>{errors.max_age}</ErrorMessage>
        ) : (
          <Description>A duration such as 168h, at most 720h. Default off.</Description>
        )}
      </Field>
      {runtime === 'firecracker' && (
        <Field disabled={disabled}>
          <Label>Firecracker VM slots</Label>
          <Input
            type="number"
            min="256"
            max="1024"
            value={form.firecracker_vm_slots}
            placeholder="256"
            invalid={errors?.firecracker_vm_slots !== undefined}
            onChange={(event) => onChange('firecracker_vm_slots', event.currentTarget.value)}
          />
          {errors?.firecracker_vm_slots ? (
            <ErrorMessage>{errors.firecracker_vm_slots}</ErrorMessage>
          ) : (
            <Description>256–1024; raising only. AerolVM keeps a pool it already seeded.</Description>
          )}
        </Field>
      )}
    </div>
  )
}

// The collapsed Advanced section used by the register dialog and the cluster
// settings dialog.
export function AdvancedOptions(props: {
  form: AdvancedForm
  onChange: (field: keyof AdvancedForm, value: string) => void
  runtime: string
  disabled?: boolean
  errors?: Partial<Record<keyof AdvancedForm, string>>
}) {
  return (
    <Disclosure title="Advanced">
      <AdvancedFields {...props} />
    </Disclosure>
  )
}

// The size caps in the create policy, or "No cap".
function sizeCapLabel(cluster: api.Cluster): string {
  const parts = [
    !!cluster.max_sandbox_cpu && `${cluster.max_sandbox_cpu} vCPU`,
    !!cluster.max_sandbox_memory_mb && `${cluster.max_sandbox_memory_mb} MB memory`,
    !!cluster.max_sandbox_disk_gb && `${cluster.max_sandbox_disk_gb} GB disk`,
  ].filter(Boolean)
  return parts.length > 0 ? `At most ${parts.join(' · ')}` : 'No cap'
}

// The lifecycle timers for sandboxes that set none of their own, or "Off".
function timersLabel(cluster: api.Cluster): string {
  const parts = [
    !!cluster.idle_stop_minutes && `Stop after ${cluster.idle_stop_minutes} min idle`,
    cluster.destroy_if_idle_for && `Delete after ${cluster.destroy_if_idle_for} idle`,
    cluster.max_age && `Delete at ${cluster.max_age} old`,
  ].filter(Boolean)
  return parts.length > 0 ? parts.join(' · ') : 'Off'
}

// The cluster's DNS, storage and advanced settings, with an Edit button.
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
        {cluster.storage_profile !== null && (
          <>
            <DescriptionTerm>Local copies of archives</DescriptionTerm>
            <DescriptionDetails>{retentionLabel(cluster.archive_local_retention)}</DescriptionDetails>
            <DescriptionTerm>Archive snapshot names</DescriptionTerm>
            <DescriptionDetails>{snapshotNameLabel(cluster.archive_snapshot_name)}</DescriptionDetails>
          </>
        )}
        <DescriptionTerm>Docker call timeout</DescriptionTerm>
        <DescriptionDetails>{cluster.docker_timeout || '15m'}</DescriptionDetails>
        <DescriptionTerm>Largest sandbox</DescriptionTerm>
        <DescriptionDetails>{sizeCapLabel(cluster)}</DescriptionDetails>
        <DescriptionTerm>Idle and age timers</DescriptionTerm>
        <DescriptionDetails>{timersLabel(cluster)}</DescriptionDetails>
        {cluster.runtime === 'firecracker' && (
          <>
            <DescriptionTerm>Firecracker VM slots</DescriptionTerm>
            <DescriptionDetails>{cluster.firecracker_vm_slots ?? '256 (default)'}</DescriptionDetails>
          </>
        )}
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
  const [retention, setRetention] = useState(cluster.archive_local_retention)
  const [snapshotName, setSnapshotName] = useState(cluster.archive_snapshot_name)
  const [advanced, setAdvanced] = useState<AdvancedForm>(() => advancedFormFromCluster(cluster))
  const [fieldErrors, setFieldErrors] = useState<Partial<Record<keyof AdvancedForm, string>>>({})
  const dnsProfiles = useLoad('dns-profiles', () => api.listDNSProfiles())
  const storageProfiles = useLoad('storage-profiles', () => api.listStorageProfiles())

  const blocked = storageProfile === '' ? 'Choose a storage profile first.' : ''
  const policy = blocked ? 'keep' : stopPolicy

  const changes: api.ClusterUpdate = {}
  if (domain !== cluster.domain) changes.domain = domain
  if (dnsProfile !== (cluster.dns_profile ?? '')) changes.dns_profile = dnsProfile || 'none'
  if (dnsRecords !== cluster.dns_records) changes.dns_records = dnsRecords
  if (storageProfile !== (cluster.storage_profile ?? '')) changes.storage_profile = storageProfile || 'none'
  if (policy !== cluster.stop_policy) changes.stop_policy = policy
  if (retention !== cluster.archive_local_retention) changes.archive_local_retention = retention
  if (snapshotName !== cluster.archive_snapshot_name) changes.archive_snapshot_name = snapshotName
  Object.assign(changes, advancedChanges(advanced, cluster))
  const changed = Object.keys(changes).length > 0
  const domainProblem = domain !== '' && !validDomain(domain)

  const save = useAction(
    () => {
      setFieldErrors({})
      return api.updateCluster(cluster.slug, changes)
    },
    {
      onError: (failure) => {
        const found = advancedFieldError(failure)
        if (!found) return false
        setFieldErrors({ [found.field]: found.message })
        return true
      },
      toError: (failure) => ({ message: failureMessage(failure) }),
    },
  )

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
            <TextLink href={routes.admin.settings('dns')}>Settings → DNS profiles</TextLink>
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
            <TextLink href={routes.admin.settings('storage')}>Settings → Storage profiles</TextLink>
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
        {storageProfile !== '' && (
          <ArchiveOptions
            retention={retention}
            onRetention={setRetention}
            snapshotName={snapshotName}
            onSnapshotName={setSnapshotName}
            disabled={save.busy}
          />
        )}
        <AdvancedOptions
          form={advanced}
          onChange={(field, value) => setAdvanced((current) => ({ ...current, [field]: value }))}
          runtime={cluster.runtime}
          disabled={save.busy}
          errors={fieldErrors}
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
