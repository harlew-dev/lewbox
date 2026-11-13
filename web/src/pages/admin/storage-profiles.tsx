import {
  Button,
  Checkbox,
  Code,
  ConfirmationDialog,
  Description,
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
  EmptyState,
  ErrorNotice,
  Field,
  Input,
  Label,
  Listbox,
  ListboxOption,
  ListSkeleton,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Text,
  toast,
  useAction,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { PencilIcon, TrashIcon } from '@heroicons/react/16/solid'
import { useState, type FormEvent } from 'react'
import * as api from '../../lib/api'
import { failureMessage, useLoad } from '../../lib/use-load'
import { LastTest, masked, saveError } from './profile-common'

// Saved buckets where clusters keep snapshots and archived sandboxes.
export function StorageProfiles() {
  const profiles = useLoad(() => api.listStorageProfiles(), [])
  const [editing, setEditing] = useState<api.StorageProfile | 'new' | null>(null)
  const [deleting, setDeleting] = useState<api.StorageProfile | null>(null)
  const [testing, setTesting] = useState('')
  const list = profiles.data?.data ?? []

  async function test(profile: api.StorageProfile) {
    setTesting(profile.name)
    try {
      const result = await api.testStorageProfile(profile.name)
      if (result.test_error) toast.error(`${profile.name}: ${result.test_error}`)
      else toast.success(`${profile.name} passed its test.`)
    } catch (failure) {
      toast.error(failureMessage(failure))
    } finally {
      setTesting('')
      profiles.refresh()
    }
  }

  return (
    <>
      <div className="mb-6 flex items-start justify-between gap-4">
        <Text className="max-w-2xl">
          A storage profile is an S3-compatible bucket with its credentials. A cluster that uses one keeps its snapshots
          there and can archive stopped sandboxes to it.
        </Text>
        <Button onClick={() => setEditing('new')}>New storage profile</Button>
      </div>

      {profiles.loading && !profiles.data ? (
        <ListSkeleton label="Loading storage profiles" rows={3} />
      ) : profiles.error.message ? (
        <ErrorNotice error={profiles.error} onRetry={profiles.reload} />
      ) : list.length === 0 ? (
        <EmptyState
          title="No storage profiles"
          description="Add a bucket to keep snapshots and archive stopped sandboxes."
          action={<Button onClick={() => setEditing('new')}>New storage profile</Button>}
        />
      ) : (
        <Table>
          <TableHead>
            <TableRow>
              <TableHeader>Name</TableHeader>
              <TableHeader>Bucket</TableHeader>
              <TableHeader>Endpoint</TableHeader>
              <TableHeader>Secret</TableHeader>
              <TableHeader>Clusters</TableHeader>
              <TableHeader>Last test</TableHeader>
              <TableHeader className="w-0">
                <span className="sr-only">Actions</span>
              </TableHeader>
            </TableRow>
          </TableHead>
          <TableBody>
            {list.map((profile) => (
              <TableRow key={profile.id}>
                <TableCell className="font-medium">{profile.name}</TableCell>
                <TableCell>{profile.bucket}</TableCell>
                <TableCell className="text-zinc-500 dark:text-zinc-400">
                  {profile.endpoint || `AWS S3 (${profile.region})`}
                </TableCell>
                <TableCell>
                  <Code>{masked(profile.secret_last4)}</Code>
                </TableCell>
                <TableCell className="tabular-nums">{profile.clusters}</TableCell>
                <TableCell className="whitespace-normal">
                  <LastTest testedAt={profile.tested_at} error={profile.test_error} />
                </TableCell>
                <TableCell className="w-0 text-right whitespace-nowrap">
                  <Button
                    plain
                    loading={testing === profile.name}
                    aria-label={`Test ${profile.name}`}
                    onClick={() => void test(profile)}
                  >
                    Test
                  </Button>
                  <Button plain aria-label={`Edit ${profile.name}`} onClick={() => setEditing(profile)}>
                    <PencilIcon />
                  </Button>
                  <Button plain aria-label={`Delete ${profile.name}`} onClick={() => setDeleting(profile)}>
                    <TrashIcon />
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <StorageProfileDialog
        open={editing !== null}
        profile={editing === 'new' ? null : editing}
        onClose={() => setEditing(null)}
        onSaved={profiles.refresh}
      />
      <ConfirmationDialog
        open={deleting !== null}
        onClose={() => setDeleting(null)}
        title={deleting ? `Delete ${deleting.name}?` : 'Delete storage profile?'}
        description="The bucket and its contents are not touched. A profile that clusters still use can’t be deleted."
        confirm="Delete profile"
        variant="danger"
        onConfirm={async () => {
          if (deleting) await api.deleteStorageProfile(deleting.name)
        }}
        onConfirmed={() => {
          toast.success('Storage profile deleted.')
          profiles.refresh()
        }}
      />
    </>
  )
}

type Preset = 'aws' | 'r2' | 'minio'

const R2_SUFFIX = '.r2.cloudflarestorage.com'

function r2Endpoint(accountId: string): string {
  return accountId ? `https://${accountId}${R2_SUFFIX}` : ''
}

function presetOf(profile: api.StorageProfile | null): Preset {
  if (!profile || profile.endpoint === '') return 'aws'
  return profile.endpoint.endsWith(R2_SUFFIX) ? 'r2' : 'minio'
}

function accountOf(profile: api.StorageProfile | null): string {
  const match = profile?.endpoint.match(/^https:\/\/([^./]+)\.r2\.cloudflarestorage\.com\/?$/)
  return match?.[1] ?? ''
}

function StorageProfileDialog({
  open,
  profile,
  onClose,
  onSaved,
}: {
  open: boolean
  profile: api.StorageProfile | null
  onClose: () => void
  onSaved: () => void
}) {
  const count = useOpenCount(open)
  return (
    <Dialog open={open} onClose={onClose} size="xl">
      <StorageProfileForm key={count} profile={profile} onClose={onClose} onSaved={onSaved} />
    </Dialog>
  )
}

function StorageProfileForm({
  profile,
  onClose,
  onSaved,
}: {
  profile: api.StorageProfile | null
  onClose: () => void
  onSaved: () => void
}) {
  const [preset, setPreset] = useState<Preset>(presetOf(profile))
  const [name, setName] = useState(profile?.name ?? '')
  const [accountId, setAccountId] = useState(accountOf(profile))
  const [endpoint, setEndpoint] = useState(profile?.endpoint ?? '')
  const [region, setRegion] = useState(profile?.region ?? '')
  const [pathStyle, setPathStyle] = useState(profile?.path_style ?? false)
  const [bucket, setBucket] = useState(profile?.bucket ?? '')
  const [prefix, setPrefix] = useState(profile?.prefix ?? '')
  const [accessKey, setAccessKey] = useState(profile?.access_key_id ?? '')
  const [secret, setSecret] = useState('')

  // A preset only pre-fills the fields; each stays editable.
  function choose(next: Preset) {
    setPreset(next)
    if (next === 'aws') {
      setEndpoint('')
      setPathStyle(false)
      if (region === 'auto') setRegion('')
    } else if (next === 'r2') {
      setEndpoint(r2Endpoint(accountId))
      setRegion('auto')
      setPathStyle(true)
    } else {
      if (endpoint.endsWith(R2_SUFFIX)) setEndpoint('')
      if (region === '' || region === 'auto') setRegion('us-east-1')
      setPathStyle(true)
    }
  }

  function changeAccount(value: string) {
    setAccountId(value)
    setEndpoint(r2Endpoint(value))
  }

  const save = useAction(
    async () => {
      if (!profile) {
        return api.createStorageProfile({
          name,
          endpoint,
          region,
          bucket,
          prefix,
          path_style: pathStyle,
          access_key_id: accessKey,
          secret_access_key: secret,
        })
      }
      // Only what changed; an empty secret keeps the current one.
      const changes: api.StorageProfileInput = {}
      if (name !== profile.name) changes.name = name
      if (endpoint !== profile.endpoint) changes.endpoint = endpoint
      if (region !== profile.region) changes.region = region
      if (bucket !== profile.bucket) changes.bucket = bucket
      if (prefix !== profile.prefix) changes.prefix = prefix
      if (pathStyle !== profile.path_style) changes.path_style = pathStyle
      if (accessKey !== profile.access_key_id) changes.access_key_id = accessKey
      if (secret !== '') changes.secret_access_key = secret
      return api.updateStorageProfile(profile.name, changes)
    },
    { toError: saveError },
  )

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await save.run()
    if (result.ok) {
      toast.success(profile ? 'Storage profile saved.' : 'Storage profile created.')
      onSaved()
      onClose()
    }
  }

  const needsEndpoint = preset !== 'aws'
  const missing =
    name.trim() === '' ||
    bucket.trim() === '' ||
    region.trim() === '' ||
    accessKey.trim() === '' ||
    (!profile && secret === '') ||
    (needsEndpoint && endpoint.trim() === '')

  return (
    <form onSubmit={submit}>
      <DialogTitle>{profile ? `Edit ${profile.name}` : 'New storage profile'}</DialogTitle>
      <DialogDescription>
        Saving tests the bucket: Lewbox writes, reads, lists and deletes a small object.
      </DialogDescription>
      <DialogBody className="space-y-6">
        <Field disabled={save.busy}>
          <Label>Provider</Label>
          <Listbox value={preset} onChange={choose}>
            <ListboxOption value="aws">AWS S3</ListboxOption>
            <ListboxOption value="r2">Cloudflare R2</ListboxOption>
            <ListboxOption value="minio">MinIO / S3-compatible</ListboxOption>
          </Listbox>
          <Description>Fills in the endpoint, region and addressing style. You can still change them.</Description>
        </Field>
        <Field disabled={save.busy}>
          <Label>Name</Label>
          <Input required value={name} placeholder="s3-main" onChange={(e) => setName(e.currentTarget.value.trim())} />
        </Field>
        {preset === 'r2' && (
          <Field disabled={save.busy}>
            <Label>Account ID</Label>
            <Input
              value={accountId}
              placeholder="0123456789abcdef0123456789abcdef"
              onChange={(e) => changeAccount(e.currentTarget.value.trim())}
            />
            <Description>{endpoint ? `Endpoint: ${endpoint}` : 'Your Cloudflare account ID.'}</Description>
          </Field>
        )}
        {preset === 'minio' && (
          <Field disabled={save.busy}>
            <Label>Endpoint</Label>
            <Input
              value={endpoint}
              placeholder="https://minio.example.com"
              onChange={(e) => setEndpoint(e.currentTarget.value.trim())}
            />
          </Field>
        )}
        <div className="grid gap-6 sm:grid-cols-2">
          <Field disabled={save.busy}>
            <Label>Region</Label>
            <Input required value={region} placeholder="us-east-1" onChange={(e) => setRegion(e.currentTarget.value.trim())} />
          </Field>
          <Field disabled={save.busy}>
            <Label>Bucket</Label>
            <Input required value={bucket} onChange={(e) => setBucket(e.currentTarget.value.trim())} />
          </Field>
        </div>
        <Field disabled={save.busy}>
          <Label>Prefix</Label>
          <Input value={prefix} placeholder="lewbox/" onChange={(e) => setPrefix(e.currentTarget.value.trim())} />
          <Description>A folder inside the bucket. Optional.</Description>
        </Field>
        <Checkbox checked={pathStyle} onChange={setPathStyle} disabled={save.busy}>
          Path-style addressing
        </Checkbox>
        <div className="grid gap-6 sm:grid-cols-2">
          <Field disabled={save.busy}>
            <Label>Access key ID</Label>
            <Input required value={accessKey} onChange={(e) => setAccessKey(e.currentTarget.value.trim())} />
          </Field>
          <Field disabled={save.busy}>
            <Label>Secret access key</Label>
            <Input
              type="password"
              autoComplete="off"
              value={secret}
              placeholder={profile ? `…${profile.secret_last4}` : ''}
              onChange={(e) => setSecret(e.currentTarget.value.trim())}
            />
            {profile && <Description>Leave empty to keep the current secret.</Description>}
          </Field>
        </div>
        <ErrorNotice error={save.error} />
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={save.busy}>
          Cancel
        </Button>
        <Button type="submit" loading={save.loading} disabled={missing} disabledReason="Fill in required fields">
          {profile ? 'Save and test' : 'Create and test'}
        </Button>
      </DialogActions>
    </form>
  )
}
