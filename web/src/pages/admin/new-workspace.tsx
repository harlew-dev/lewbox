import {
  Button,
  Description,
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
  ErrorNotice,
  Field,
  FieldGroup,
  Fieldset,
  Input,
  Label,
  Legend,
  Listbox,
  ListboxOption,
  ListSkeleton,
  Notice,
  Text,
  toast,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { useRef, useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router'
import { useRefreshPending } from '../../components/console-layout'
import * as api from '../../lib/api'
import { failureMessage, useLoad } from '../../lib/use-load'

const lower = 'abcdefghijklmnopqrstuvwxyz'
const digits = '0123456789'

// `length` characters from `alphabet`, uniformly, from crypto.getRandomValues.
export function randomString(length: number, alphabet: string): string {
  const limit = 256 - (256 % alphabet.length)
  let out = ''
  while (out.length < length) {
    const bytes = crypto.getRandomValues(new Uint8Array(length * 2))
    for (const byte of bytes) {
      if (byte < limit && out.length < length) out += alphabet[byte % alphabet.length]
    }
  }
  return out
}

// A strong password to hand to the workspace's owner: 20 letters and digits.
export function generatePassword(): string {
  return randomString(20, lower + lower.toUpperCase() + digits)
}

// The owner's account: 'existing' when the email already has one (the
// password is then unused), otherwise created with `password`.
async function ownerAccount(email: string, password: string): Promise<'created' | 'existing'> {
  try {
    await api.getUser(email)
    return 'existing'
  } catch (failure) {
    if (!(failure instanceof api.ApiError && failure.status === 404)) throw failure
  }
  if (password === '') throw new Error(`${email} has no account yet. Enter a password for it.`)
  try {
    await api.createUser({ email, password, instance_admin: false })
    return 'created'
  } catch (failure) {
    if (failure instanceof api.ApiError && failure.status === 409) return 'existing'
    throw failure
  }
}

// What has been made so far, so a failure says which step failed and a
// second try picks up from there rather than making anything twice. What
// was made is locked in the form, so a second try can't ignore an edit.
interface Progress {
  account: 'created' | 'existing' | null
  workspace: api.Organization | null
  addedRegion: boolean
  failed: { step: string; message: string } | null
}

const started: Progress = { account: null, workspace: null, addedRegion: false, failed: null }

// `onChanged` runs when the dialog closes, however it closes, once it has
// made a workspace.
export function NewWorkspaceDialog({
  open,
  onClose,
  onChanged,
}: {
  open: boolean
  onClose: () => void
  onChanged: () => void
}) {
  const count = useOpenCount(open)
  const made = useRef(false)
  function close() {
    if (made.current) onChanged()
    made.current = false
    onClose()
  }
  return (
    <Dialog open={open} onClose={close} size="2xl">
      <NewWorkspace key={count} onClose={close} onMade={() => (made.current = true)} />
    </Dialog>
  )
}

function NewWorkspace({ onClose, onMade }: { onClose: () => void; onMade: () => void }) {
  const settings = useLoad(() => api.getSettings(), [])
  const regions = useLoad(() => api.listRegions(), [])
  const plans = useLoad(() => api.listPlans(), [])

  if ((settings.loading && !settings.data) || (regions.loading && !regions.data) || (plans.loading && !plans.data)) {
    return (
      <>
        <DialogTitle>New workspace</DialogTitle>
        <DialogBody>
          <ListSkeleton label="Loading" rows={3} />
        </DialogBody>
      </>
    )
  }
  const failed = [settings, regions, plans].find((load) => load.error.message)
  if (failed || !settings.data || !regions.data || !plans.data) {
    return (
      <>
        <DialogTitle>New workspace</DialogTitle>
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
  return (
    <NewWorkspaceForm
      settings={settings.data}
      regions={regions.data.data}
      plans={plans.data.data}
      onClose={onClose}
      onMade={onMade}
    />
  )
}

function NewWorkspaceForm({
  settings,
  regions,
  plans,
  onClose,
  onMade,
}: {
  settings: api.Settings
  regions: api.Region[]
  plans: api.Plan[]
  onClose: () => void
  onMade: () => void
}) {
  const navigate = useNavigate()
  const refreshPending = useRefreshPending()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [name, setName] = useState('')
  const defaultRegion = settings.default_region ?? ''
  const [region, setRegion] = useState(
    regions.some((option) => option.slug === defaultRegion) ? defaultRegion : (regions[0]?.slug ?? ''),
  )
  const [plan, setPlan] = useState(settings.default_plan ?? plans[0]?.slug ?? '')
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState<Progress>(started)

  const regionName = (slug: string) => regions.find((option) => option.slug === slug)?.name ?? slug
  const complete = region !== '' && plan !== ''
  const ready = email.trim() !== '' && name.trim() !== '' && (regions.length === 0 || complete)

  function open(slug: string) {
    onClose()
    navigate(`/admin/workspaces/${encodeURIComponent(slug)}`)
  }

  // Finds or creates the account, then the workspace with it as owner, then
  // its plan in the cluster, in that order. A failure stops there and keeps
  // what was made: nothing is rolled back.
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (busy || !ready) return
    setBusy(true)
    const next: Progress = { ...progress, failed: null }
    let step = ''
    try {
      if (!next.account) {
        step = 'Creating the account'
        next.account = await ownerAccount(email.trim(), password)
      }
      if (!next.workspace) {
        step = 'Creating the workspace'
        next.workspace = await api.createOrganization({ name: name.trim(), owner: email.trim() })
        onMade()
      }
      if (!next.addedRegion && region !== '' && plan !== '') {
        step = `Adding ${regionName(region)}`
        await api.setSubscription(next.workspace.slug, region, { plan })
        next.addedRegion = true
      }
    } catch (failure) {
      setProgress({ ...next, failed: { step, message: failureMessage(failure) } })
      if (next.workspace) refreshPending()
      setBusy(false)
      return
    }
    setBusy(false)
    refreshPending()
    toast.success(`${next.workspace.name} created.`)
    open(next.workspace.slug)
  }

  const made = [
    progress.account === 'created' && `Created the account ${email.trim()}.`,
    progress.account === 'existing' && `Used the existing account ${email.trim()}.`,
    progress.workspace && `Created the workspace ${progress.workspace.name}.`,
    progress.addedRegion && `Added ${regionName(region)}.`,
  ].filter((line): line is string => typeof line === 'string')

  const accountLocked = busy || progress.account !== null
  const nameLocked = busy || progress.workspace !== null
  const regionLocked = busy || progress.addedRegion

  return (
    <form onSubmit={submit}>
      <DialogTitle>New workspace</DialogTitle>
      <DialogDescription>An owner account, a workspace and its cluster and plan.</DialogDescription>
      <DialogBody>
        <FieldGroup>
          {progress.failed && (
            <Notice variant="error">
              <p>
                <strong className="font-semibold">{progress.failed.step} failed:</strong> {progress.failed.message}
              </p>
              {made.length > 0 && (
                <ul className="mt-2 list-disc pl-5">
                  {made.map((line) => (
                    <li key={line}>{line}</li>
                  ))}
                </ul>
              )}
              {progress.workspace && <p className="mt-2">Try again, or finish on the workspace’s page.</p>}
            </Notice>
          )}
          <div className="grid gap-4 sm:grid-cols-2">
            <Field disabled={accountLocked}>
              <Label>Owner email</Label>
              <Input
                type="email"
                required
                autoFocus
                value={email}
                placeholder="dev@acme.com"
                onChange={(event) => setEmail(event.currentTarget.value)}
              />
            </Field>
            <Field disabled={nameLocked}>
              <Label>Workspace name</Label>
              <Input required value={name} placeholder="Acme" onChange={(event) => setName(event.currentTarget.value)} />
            </Field>
          </div>
          <Field disabled={accountLocked}>
            <Label>Password</Label>
            <div className="flex gap-2">
              <div className="flex-1">
                <Input
                  autoComplete="off"
                  spellCheck={false}
                  value={password}
                  onChange={(event) => setPassword(event.currentTarget.value)}
                  className="font-mono"
                />
              </div>
              <Button outline onClick={() => setPassword(generatePassword())} disabled={accountLocked}>
                Generate
              </Button>
            </div>
            <Description>Only for a new account. Copy it for the owner.</Description>
          </Field>

          <Fieldset disabled={regionLocked}>
            <Legend>Cluster and plan</Legend>
            {regions.length === 0 ? (
              <Text className="mt-2">No cluster is ready yet. Add one on the Servers page first.</Text>
            ) : (
              <div className="mt-3 flex items-center gap-2">
                <Listbox
                  aria-label="Cluster"
                  value={region}
                  onChange={setRegion}
                  disabled={regionLocked}
                  placeholder="Cluster"
                  className="flex-1"
                >
                  {regions.map((option) => (
                    <ListboxOption key={option.slug} value={option.slug}>
                      {option.name}
                    </ListboxOption>
                  ))}
                </Listbox>
                <Listbox
                  aria-label="Plan"
                  value={plan}
                  onChange={setPlan}
                  disabled={regionLocked}
                  placeholder="Plan"
                  className="flex-1"
                >
                  {plans.map((option) => (
                    <ListboxOption key={option.slug} value={option.slug}>
                      {option.name}
                    </ListboxOption>
                  ))}
                </Listbox>
              </div>
            )}
            {regions.length > 0 && (region === '' || plan === '') && (
              <Text className="mt-2">Choose a cluster and a plan, or leave both empty for no region yet.</Text>
            )}
          </Fieldset>
        </FieldGroup>
      </DialogBody>
      <DialogActions>
        {progress.workspace ? (
          <Button plain onClick={() => open(progress.workspace!.slug)} disabled={busy}>
            Open {progress.workspace.name}
          </Button>
        ) : (
          <Button plain onClick={onClose} disabled={busy}>
            Cancel
          </Button>
        )}
        <Button type="submit" loading={busy} disabled={!ready} disabledReason="Fill in required fields">
          {progress.failed ? 'Try again' : 'Create workspace'}
        </Button>
      </DialogActions>
    </form>
  )
}
