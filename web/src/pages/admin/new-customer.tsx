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
import { PlusIcon, XMarkIcon } from '@heroicons/react/16/solid'
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

// A strong password to hand to the customer: 20 letters and digits.
export function generatePassword(): string {
  return randomString(20, lower + lower.toUpperCase() + digits)
}

// A workspace slug from its name: lowercase letters, digits and dashes, at
// most 40 of them, then a dash and 4 random characters so it is unique.
export function workspaceSlug(name: string, suffix = randomString(4, lower + digits)): string {
  const base =
    name
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+/, '')
      .slice(0, 40)
      .replace(/-+$/, '') || 'workspace'
  return `${base}-${suffix}`
}

interface Row {
  id: number
  region: string
  plan: string
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
  // Region slugs given their plan.
  regions: string[]
  failed: { step: string; message: string } | null
}

const started: Progress = { account: null, workspace: null, regions: [], failed: null }

// `onChanged` runs when the dialog closes, however it closes, once it has
// made a workspace.
export function NewCustomerDialog({
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
      <NewCustomer key={count} onClose={close} onMade={() => (made.current = true)} />
    </Dialog>
  )
}

function NewCustomer({ onClose, onMade }: { onClose: () => void; onMade: () => void }) {
  const settings = useLoad(() => api.getSettings(), [])
  const regions = useLoad(() => api.listRegions(), [])
  const plans = useLoad(() => api.listPlans(), [])

  if ((settings.loading && !settings.data) || (regions.loading && !regions.data) || (plans.loading && !plans.data)) {
    return (
      <>
        <DialogTitle>New customer</DialogTitle>
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
        <DialogTitle>New customer</DialogTitle>
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
    <NewCustomerForm
      settings={settings.data}
      regions={regions.data.data}
      plans={plans.data.data}
      onClose={onClose}
      onMade={onMade}
    />
  )
}

function NewCustomerForm({
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
  const [rows, setRows] = useState<Row[]>(() => {
    const plan = settings.default_plan ?? ''
    if (!plan) return []
    return settings.default_regions
      .filter((slug) => regions.some((region) => region.slug === slug))
      .map((region, index) => ({ id: index, region, plan }))
  })
  const [nextId, setNextId] = useState(rows.length)
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState<Progress>(started)

  const regionName = (slug: string) => regions.find((region) => region.slug === slug)?.name ?? slug
  const complete = rows.every((row) => row.region !== '' && row.plan !== '')
  const ready = email.trim() !== '' && name.trim() !== '' && complete

  function change(id: number, field: 'region' | 'plan', value: string) {
    setRows((before) => before.map((row) => (row.id === id ? { ...row, [field]: value } : row)))
  }

  function addRow() {
    const used = new Set(rows.map((row) => row.region))
    const region = regions.find((option) => !used.has(option.slug))?.slug ?? ''
    setRows((before) => [...before, { id: nextId, region, plan: settings.default_plan ?? plans[0]?.slug ?? '' }])
    setNextId((value) => value + 1)
  }

  function open(slug: string) {
    onClose()
    navigate(`/admin/customers/${encodeURIComponent(slug)}`)
  }

  // Finds or creates the account, then the workspace with it as owner,
  // then the plan in each region, in that order. A failure stops there and
  // keeps what was made: nothing is rolled back.
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (busy || !ready) return
    setBusy(true)
    const next: Progress = { ...progress, regions: [...progress.regions], failed: null }
    let step = ''
    try {
      if (!next.account) {
        step = 'Creating the account'
        next.account = await ownerAccount(email.trim(), password)
      }
      if (!next.workspace) {
        step = 'Creating the workspace'
        next.workspace = await api.createOrganization({ slug: workspaceSlug(name), name: name.trim(), owner: email.trim() })
        onMade()
      }
      for (const row of rows) {
        if (next.regions.includes(row.region)) continue
        step = `Adding ${regionName(row.region)}`
        await api.setSubscription(next.workspace.slug, row.region, { plan: row.plan })
        next.regions.push(row.region)
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
    ...progress.regions.map((slug) => `Added ${regionName(slug)}.`),
  ].filter((line): line is string => typeof line === 'string')

  const accountLocked = busy || progress.account !== null
  const nameLocked = busy || progress.workspace !== null
  const added = new Set(progress.regions)
  const regionsFree = regions.length > rows.length

  return (
    <form onSubmit={submit}>
      <DialogTitle>New customer</DialogTitle>
      <DialogDescription>An owner account, a workspace and its regions.</DialogDescription>
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
              {progress.workspace && <p className="mt-2">Try again, or finish on the customer’s page.</p>}
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
            <Description>Only for a new account. Copy it for the customer.</Description>
          </Field>

          <Fieldset disabled={busy}>
            <Legend>Regions</Legend>
            {regions.length === 0 ? (
              <Text className="mt-2">No region is ready yet. Add one on the Servers page first.</Text>
            ) : (
              <div className="mt-3 space-y-3">
                {rows.length === 0 && <Text>No regions: it can’t run sandboxes yet.</Text>}
                {rows.map((row, index) => {
                  const taken = new Set(rows.filter((other) => other.id !== row.id).map((other) => other.region))
                  const done = added.has(row.region)
                  return (
                    <div key={row.id} className="flex items-center gap-2">
                      <Listbox
                        disabled={done}
                        aria-label={`Region ${index + 1}`}
                        value={row.region}
                        onChange={(value) => change(row.id, 'region', value)}
                        placeholder="Region"
                        className="flex-1"
                      >
                        {regions
                          .filter((region) => !taken.has(region.slug))
                          .map((region) => (
                            <ListboxOption key={region.slug} value={region.slug}>
                              {region.name}
                            </ListboxOption>
                          ))}
                      </Listbox>
                      <Listbox
                        disabled={done}
                        aria-label={`Plan in region ${index + 1}`}
                        value={row.plan}
                        onChange={(value) => change(row.id, 'plan', value)}
                        placeholder="Plan"
                        className="flex-1"
                      >
                        {plans.map((plan) => (
                          <ListboxOption key={plan.slug} value={plan.slug}>
                            {plan.name}
                          </ListboxOption>
                        ))}
                      </Listbox>
                      <Button
                        plain
                        disabled={done}
                        disabledReason="Already added"
                        aria-label={`Remove region ${index + 1}`}
                        onClick={() => setRows((before) => before.filter((other) => other.id !== row.id))}
                      >
                        <XMarkIcon />
                      </Button>
                    </div>
                  )
                })}
                {regionsFree && plans.length > 0 && (
                  <Button plain onClick={addRow}>
                    <PlusIcon />
                    Add region
                  </Button>
                )}
              </div>
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
          {progress.failed ? 'Try again' : 'Create customer'}
        </Button>
      </DialogActions>
    </form>
  )
}
