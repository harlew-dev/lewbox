import {
  Badge,
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
  ErrorNotice,
  Field,
  FieldGroup,
  Input,
  Label,
  Listbox,
  ListboxOption,
  ListSkeleton,
  SecretReveal,
  Text,
  useAction,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { useState, type FormEvent } from 'react'
import * as api from '../../lib/api'
import { formatDate } from '../../lib/format'
import { failureMessage, useLoad } from '../../lib/use-load'

const statusColors: Record<api.SetupLinkStatus, 'lime' | 'amber' | 'zinc' | 'red'> = {
  unused: 'lime',
  claimed: 'amber',
  used: 'zinc',
  expired: 'red',
}

export function SetupLinkBadge({ status }: { status: api.SetupLinkStatus }) {
  return <Badge color={statusColors[status]}>{status.charAt(0).toUpperCase() + status.slice(1)}</Badge>
}

const expiries = [
  { value: '1', label: '1 day' },
  { value: '7', label: '7 days' },
  { value: '30', label: '30 days' },
] as const

export function CreateSetupLinkDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean
  onClose: () => void
  onCreated: () => void
}) {
  const count = useOpenCount(open)
  return (
    <Dialog open={open} onClose={onClose} size="lg">
      <CreateSetupLinkForm key={count} onClose={onClose} onCreated={onCreated} />
    </Dialog>
  )
}

function CreateSetupLinkForm({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const settings = useLoad(() => api.getSettings(), [])
  const regions = useLoad(() => api.listRegions(), [])
  const plans = useLoad(() => api.listPlans(), [])
  const [created, setCreated] = useState<api.SetupLink | null>(null)
  const [region, setRegion] = useState('')
  const [plan, setPlan] = useState('')
  const [expires, setExpires] = useState('7')
  const [note, setNote] = useState('')

  const loading =
    (settings.loading && !settings.data) ||
    (regions.loading && !regions.data) ||
    (plans.loading && !plans.data)
  const failed = [settings, regions, plans].find((load) => load.error.message)

  const create = useAction(
    (input: api.SetupLinkInput) => api.createSetupLink(input),
    { toError: (failure) => ({ message: failureMessage(failure) }) },
  )

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await create.run({
      plan: chosenPlan,
      region: chosenRegion,
      expires_in_hours: Number(expires) * 24,
      note: note.trim(),
    })
    if (!result.ok) return
    setCreated(result.value)
    onCreated()
  }

  if (created) {
    const url = `${window.location.origin}/setup/${created.token ?? ''}`
    return (
      <>
        <DialogTitle>Setup link created</DialogTitle>
        <DialogDescription>Send it to your customer. It is shown only once.</DialogDescription>
        <DialogBody>
          <SecretReveal secret={url} label="Copy setup link">
            <Text>
              Send this link to your customer. It works once, until {formatDate(created.expires_at)}.
            </Text>
          </SecretReveal>
        </DialogBody>
        <DialogActions>
          <Button onClick={onClose}>Done</Button>
        </DialogActions>
      </>
    )
  }

  if (loading) {
    return (
      <>
        <DialogTitle>Create setup link</DialogTitle>
        <DialogBody>
          <ListSkeleton label="Loading" rows={3} />
        </DialogBody>
      </>
    )
  }

  if (failed || !settings.data || !regions.data || !plans.data) {
    return (
      <>
        <DialogTitle>Create setup link</DialogTitle>
        <DialogBody>
          <ErrorNotice
            error={failed?.error ?? { message: 'Could not load the form.', key: 0 }}
            onRetry={() => [settings, regions, plans].forEach((load) => load.reload())}
          />
        </DialogBody>
        <DialogActions>
          <Button plain onClick={onClose}>
            Cancel
          </Button>
        </DialogActions>
      </>
    )
  }

  const regionOptions = regions.data.data
  const planOptions = plans.data.data
  const settingsData = settings.data
  const defaultRegion =
    regionOptions.find((option) => option.slug === settingsData.default_region)?.slug ??
    regionOptions[0]?.slug ??
    ''
  const defaultPlan =
    planOptions.find((option) => option.slug === settingsData.default_plan)?.slug ??
    planOptions.find((option) => option.default)?.slug ??
    planOptions[0]?.slug ??
    ''
  const chosenRegion = region || defaultRegion
  const chosenPlan = plan || defaultPlan
  const ready = chosenRegion !== '' && chosenPlan !== ''

  return (
    <form onSubmit={submit}>
      <DialogTitle>Create setup link</DialogTitle>
      <DialogDescription>
        A single-use link that registers an account, creates a workspace and gives it a plan in a region.
      </DialogDescription>
      <DialogBody>
        <FieldGroup>
          <ErrorNotice error={create.error} />
          <div className="grid gap-4 sm:grid-cols-2">
            <Field disabled={create.busy}>
              <Label>Cluster</Label>
              <Listbox
                aria-label="Cluster"
                value={chosenRegion}
                onChange={(value) => setRegion(value ?? '')}
                placeholder="Cluster"
              >
                {regionOptions.map((option) => (
                  <ListboxOption key={option.slug} value={option.slug}>
                    {option.name}
                  </ListboxOption>
                ))}
              </Listbox>
            </Field>
            <Field disabled={create.busy}>
              <Label>Plan</Label>
              <Listbox
                aria-label="Plan"
                value={chosenPlan}
                onChange={(value) => setPlan(value ?? '')}
                placeholder="Plan"
              >
                {planOptions.map((option) => (
                  <ListboxOption key={option.slug} value={option.slug}>
                    {option.name}
                  </ListboxOption>
                ))}
              </Listbox>
            </Field>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field disabled={create.busy}>
              <Label>Expires</Label>
              <Listbox aria-label="Expires" value={expires} onChange={(value) => setExpires(value ?? '7')}>
                {expiries.map((option) => (
                  <ListboxOption key={option.value} value={option.value}>
                    {option.label}
                  </ListboxOption>
                ))}
              </Listbox>
            </Field>
            <Field disabled={create.busy}>
              <Label>Note</Label>
              <Input
                value={note}
                placeholder="For Acme’s new team"
                onChange={(event) => setNote(event.currentTarget.value)}
              />
            </Field>
          </div>
          {(regionOptions.length === 0 || planOptions.length === 0) && (
            <Text>Add a ready cluster and a plan first.</Text>
          )}
        </FieldGroup>
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={create.busy}>
          Cancel
        </Button>
        <Button type="submit" loading={create.loading} disabled={!ready} disabledReason="Choose a cluster and a plan">
          Create setup link
        </Button>
      </DialogActions>
    </form>
  )
}
