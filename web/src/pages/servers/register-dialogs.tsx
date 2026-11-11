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
  Input,
  Label,
  Listbox,
  ListboxOption,
  PasswordField,
  toast,
  useAction,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router'
import * as api from '../../lib/api'
import type { Runtime } from '../../lib/commands'
import { failureMessage } from '../../lib/use-load'

// The checks a failed registration reports in its error details.
function failedChecks(failure: unknown): api.Check[] {
  if (!(failure instanceof api.ApiError) || !Array.isArray(failure.details)) return []
  return (failure.details as api.Check[]).filter((check) => check && !check.ok)
}

// Registers an AerolVM cluster that already runs: lewbox cluster add.
export function RegisterClusterDialog({
  open,
  onClose,
}: {
  open: boolean
  onClose: () => void
}) {
  const count = useOpenCount(open)
  return (
    <Dialog open={open} onClose={onClose} size="xl">
      <RegisterCluster key={count} onClose={onClose} />
    </Dialog>
  )
}

function RegisterCluster({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate()
  const [slug, setSlug] = useState('')
  const [name, setName] = useState('')
  const [adminURL, setAdminURL] = useState('')
  const [endpointURL, setEndpointURL] = useState('')
  const [runtime, setRuntime] = useState<Runtime>('docker')
  const [token, setToken] = useState('')
  const [checks, setChecks] = useState<api.Check[]>([])

  const register = useAction(
    async () => {
      setChecks([])
      try {
        return await api.addCluster({
          slug,
          name,
          admin_url: adminURL,
          endpoint_url: endpointURL,
          runtime,
          admin_token: token,
        })
      } catch (failure) {
        setChecks(failedChecks(failure))
        throw failure
      }
    },
    { toError: (failure) => ({ message: failureMessage(failure) }) },
  )

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await register.run()
    if (result.ok && result.value) {
      toast.success(`Cluster ${result.value.name} registered.`)
      onClose()
      navigate(`/servers/clusters/${encodeURIComponent(result.value.slug)}`)
    }
  }

  return (
    <form onSubmit={submit}>
      <DialogTitle>Register an existing cluster</DialogTitle>
      <DialogDescription>
        For an AerolVM cluster that is already running. Lewbox checks it before saving, and
        keeps the admin token encrypted.
      </DialogDescription>
      <DialogBody className="space-y-6">
        <ErrorNotice error={register.error} />
        {checks.length > 0 && (
          <ul className="space-y-1 text-sm/6 text-zinc-600 dark:text-zinc-400">
            {checks.map((check) => (
              <li key={check.name}>
                <span className="font-medium text-zinc-950 dark:text-white">{check.name}</span>:{' '}
                {check.detail ?? 'failed'}
              </li>
            ))}
          </ul>
        )}
        <FieldGroup>
          <div className="grid gap-6 sm:grid-cols-2">
            <Field disabled={register.busy}>
              <Label>Slug</Label>
              <Input
                required
                value={slug}
                placeholder="eu-west"
                onChange={(event) => setSlug(event.currentTarget.value.trim())}
              />
            </Field>
            <Field disabled={register.busy}>
              <Label>Name</Label>
              <Input
                value={name}
                placeholder="EU West"
                onChange={(event) => setName(event.currentTarget.value)}
              />
            </Field>
          </div>
          <Field disabled={register.busy}>
            <Label>Admin URL</Label>
            <Input
              type="url"
              required
              value={adminURL}
              placeholder="https://eu-west.example.com:8443"
              onChange={(event) => setAdminURL(event.currentTarget.value.trim())}
            />
            <Description>Where Lewbox reaches the cluster's admin API.</Description>
          </Field>
          <Field disabled={register.busy}>
            <Label>Endpoint URL</Label>
            <Input
              type="url"
              value={endpointURL}
              placeholder="Same as the admin URL"
              onChange={(event) => setEndpointURL(event.currentTarget.value.trim())}
            />
            <Description>What your customers' apps use.</Description>
          </Field>
          <Field disabled={register.busy}>
            <Label>Runtime</Label>
            <Listbox value={runtime} onChange={setRuntime}>
              <ListboxOption value="docker">Docker</ListboxOption>
              <ListboxOption value="firecracker">Firecracker</ListboxOption>
            </Listbox>
          </Field>
          <PasswordField
            label="Admin token"
            description="An AerolVM admin token for this cluster (avm_…)."
            autoComplete="off"
            value={token}
            onChange={setToken}
            disabled={register.busy}
            autoFocus={false}
          />
        </FieldGroup>
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={register.busy}>
          Cancel
        </Button>
        <Button
          type="submit"
          loading={register.loading}
          disabled={slug === '' || adminURL === '' || token === ''}
          disabledReason="Fill in required fields"
        >
          Register cluster
        </Button>
      </DialogActions>
    </form>
  )
}

// Registers a node by its AerolVM URL, without touching a machine:
// lewbox node add <name> --url.
export function RegisterNodeDialog({
  open,
  onClose,
  onRegistered,
}: {
  open: boolean
  onClose: () => void
  onRegistered: () => void
}) {
  const count = useOpenCount(open)
  return (
    <Dialog open={open} onClose={onClose} size="md">
      <RegisterNode key={count} onClose={onClose} onRegistered={onRegistered} />
    </Dialog>
  )
}

function RegisterNode({ onClose, onRegistered }: { onClose: () => void; onRegistered: () => void }) {
  const [name, setName] = useState('')
  const [url, setURL] = useState('')
  const register = useAction(() => api.addNode({ name, url }), {
    toError: (failure) => ({ message: failureMessage(failure) }),
  })

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await register.run()
    if (result.ok) {
      toast.success(`Node ${name} registered.`)
      onRegistered()
      onClose()
    }
  }

  return (
    <form onSubmit={submit}>
      <DialogTitle>Register a node by URL</DialogTitle>
      <DialogDescription>
        Records a node that isn't in a registered cluster, without changing the machine. Nodes
        in a cluster appear on their own.
      </DialogDescription>
      <DialogBody>
        <FieldGroup>
          <ErrorNotice error={register.error} />
          <Field disabled={register.busy}>
            <Label>Name</Label>
            <Input
              required
              value={name}
              placeholder="node-1"
              onChange={(event) => setName(event.currentTarget.value.trim())}
            />
          </Field>
          <Field disabled={register.busy}>
            <Label>AerolVM URL</Label>
            <Input
              type="url"
              required
              value={url}
              placeholder="https://10.0.0.5:8443"
              onChange={(event) => setURL(event.currentTarget.value.trim())}
            />
          </Field>
        </FieldGroup>
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={register.busy}>
          Cancel
        </Button>
        <Button
          type="submit"
          loading={register.loading}
          disabled={name === '' || url === ''}
          disabledReason="Fill in required fields"
        >
          Register node
        </Button>
      </DialogActions>
    </form>
  )
}
