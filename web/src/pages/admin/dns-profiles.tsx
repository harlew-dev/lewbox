import {
  Button,
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

// Saved Cloudflare credentials that clusters use for DNS and TLS.
export function DNSProfiles() {
  const profiles = useLoad(() => api.listDNSProfiles(), [])
  const [editing, setEditing] = useState<api.DNSProfile | 'new' | null>(null)
  const [deleting, setDeleting] = useState<api.DNSProfile | null>(null)
  const [testing, setTesting] = useState('')
  const list = profiles.data?.data ?? []

  async function test(profile: api.DNSProfile) {
    setTesting(profile.name)
    try {
      const result = await api.testDNSProfile(profile.name)
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
          A DNS profile is a Cloudflare API token and the one zone it reaches. A cluster that uses a domain picks a
          profile for its TLS certificates and DNS records.
        </Text>
        <Button onClick={() => setEditing('new')}>New DNS profile</Button>
      </div>

      {profiles.loading && !profiles.data ? (
        <ListSkeleton label="Loading DNS profiles" rows={3} />
      ) : profiles.error.message ? (
        <ErrorNotice error={profiles.error} onRetry={profiles.reload} />
      ) : list.length === 0 ? (
        <EmptyState
          title="No DNS profiles"
          description="Add one to start a cluster on a domain with TLS."
          action={<Button onClick={() => setEditing('new')}>New DNS profile</Button>}
        />
      ) : (
        <Table>
          <TableHead>
            <TableRow>
              <TableHeader>Name</TableHeader>
              <TableHeader>Zone</TableHeader>
              <TableHeader>Token</TableHeader>
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
                <TableCell>{profile.zone}</TableCell>
                <TableCell>
                  <Code>{masked(profile.token_last4)}</Code>
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

      <DNSProfileDialog
        open={editing !== null}
        profile={editing === 'new' ? null : editing}
        onClose={() => setEditing(null)}
        onSaved={profiles.refresh}
      />
      <ConfirmationDialog
        open={deleting !== null}
        onClose={() => setDeleting(null)}
        title={deleting ? `Delete ${deleting.name}?` : 'Delete DNS profile?'}
        description="Clusters can no longer use it. A profile that clusters still use can’t be deleted."
        confirm="Delete profile"
        variant="danger"
        onConfirm={async () => {
          if (deleting) await api.deleteDNSProfile(deleting.name)
        }}
        onConfirmed={() => {
          toast.success('DNS profile deleted.')
          profiles.refresh()
        }}
      />
    </>
  )
}

function DNSProfileDialog({
  open,
  profile,
  onClose,
  onSaved,
}: {
  open: boolean
  profile: api.DNSProfile | null
  onClose: () => void
  onSaved: () => void
}) {
  const count = useOpenCount(open)
  return (
    <Dialog open={open} onClose={onClose}>
      <DNSProfileForm key={count} profile={profile} onClose={onClose} onSaved={onSaved} />
    </Dialog>
  )
}

function DNSProfileForm({
  profile,
  onClose,
  onSaved,
}: {
  profile: api.DNSProfile | null
  onClose: () => void
  onSaved: () => void
}) {
  const [name, setName] = useState(profile?.name ?? '')
  const [zone, setZone] = useState(profile?.zone ?? '')
  const [token, setToken] = useState('')
  const [email, setEmail] = useState(profile?.acme_email ?? '')

  const save = useAction(
    async () => {
      if (!profile) {
        return api.createDNSProfile({ name, zone, api_token: token, acme_email: email })
      }
      // Only what changed; an empty token keeps the current one.
      const changes: api.DNSProfileInput = {}
      if (name !== profile.name) changes.name = name
      if (zone !== profile.zone) changes.zone = zone
      if (token !== '') changes.api_token = token
      if (email !== profile.acme_email) changes.acme_email = email
      return api.updateDNSProfile(profile.name, changes)
    },
    { toError: saveError },
  )

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await save.run()
    if (result.ok) {
      toast.success(profile ? 'DNS profile saved.' : 'DNS profile created.')
      onSaved()
      onClose()
    }
  }

  const missing = name.trim() === '' || zone.trim() === '' || (!profile && token === '')

  return (
    <form onSubmit={submit}>
      <DialogTitle>{profile ? `Edit ${profile.name}` : 'New DNS profile'}</DialogTitle>
      <DialogDescription>
        Saving tests the token: Lewbox finds the zone, then writes and deletes a TXT record.
      </DialogDescription>
      <DialogBody className="space-y-6">
        <Field disabled={save.busy}>
          <Label>Name</Label>
          <Input required value={name} placeholder="cf-prod" onChange={(e) => setName(e.currentTarget.value.trim())} />
        </Field>
        <Field disabled={save.busy}>
          <Label>Zone</Label>
          <Input required value={zone} placeholder="example.com" onChange={(e) => setZone(e.currentTarget.value.trim())} />
        </Field>
        <Field disabled={save.busy}>
          <Label>API token</Label>
          <Input
            type="password"
            autoComplete="off"
            value={token}
            placeholder={profile ? `…${profile.token_last4}` : ''}
            onChange={(e) => setToken(e.currentTarget.value.trim())}
          />
          <Description>
            {profile ? 'Leave empty to keep the current token. ' : ''}A Cloudflare token with Zone:Read and DNS:Edit
            on that one zone.
          </Description>
        </Field>
        <Field disabled={save.busy}>
          <Label>ACME email</Label>
          <Input
            type="email"
            value={email}
            placeholder="ops@example.com"
            onChange={(e) => setEmail(e.currentTarget.value.trim())}
          />
          <Description>Let’s Encrypt’s contact for certificates. Optional.</Description>
        </Field>
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
