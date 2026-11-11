import {
  Button,
  CodeBlock,
  CodeTabs,
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
  ListSkeleton,
  Notice,
  Text,
  useAction,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { useState, type FormEvent } from 'react'
import * as api from '../../lib/api'
import { formatDate } from '../../lib/format'
import { sandboxSnippets } from '../../lib/snippets'
import { failureMessage, useLoad } from '../../lib/use-load'

// Creates a sandbox API key and shows it once, with what to paste. Once a
// key exists the dialog only closes from Done, so it isn't lost to a stray
// click.
export function CreateKeyDialog({
  open,
  org,
  onClose,
  onCreated,
}: {
  open: boolean
  org: string
  onClose: () => void
  onCreated: (key: api.NewSandboxKey) => void
}) {
  const count = useOpenCount(open)
  const [created, setCreated] = useState(false)
  return (
    <Dialog open={open} onClose={created ? () => {} : onClose} size="2xl">
      <CreateKey
        key={count}
        org={org}
        onClose={() => {
          setCreated(false)
          onClose()
        }}
        onCreated={(key) => {
          setCreated(true)
          onCreated(key)
        }}
      />
    </Dialog>
  )
}

function CreateKey({
  org,
  onClose,
  onCreated,
}: {
  org: string
  onClose: () => void
  onCreated: (key: api.NewSandboxKey) => void
}) {
  const regions = useLoad(() => api.listRegions(), [])
  const list = regions.data?.data ?? []
  const [name, setName] = useState('')
  const [picked, setPicked] = useState('')
  const [expiresIn, setExpiresIn] = useState('90d')
  const [key, setKey] = useState<api.NewSandboxKey | null>(null)
  const region = picked || list[0]?.slug || ''
  const create = useAction(() => api.createSandboxKey(org, { name: name.trim(), region, expires_in: expiresIn }), {
    toError: (failure) => ({ message: failureMessage(failure) }),
  })

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await create.run()
    if (result.ok) {
      setKey(result.value)
      onCreated(result.value)
    }
  }

  if (key) return <NewKey apiKey={key} onDone={onClose} />

  return (
    <form onSubmit={submit}>
      <DialogTitle>Create an API key</DialogTitle>
      <DialogDescription>A key works in one region, for this organization’s sandboxes.</DialogDescription>
      <DialogBody>
        {regions.loading && !regions.data ? (
          <ListSkeleton label="Loading regions" rows={2} />
        ) : regions.error.message ? (
          <ErrorNotice error={regions.error} onRetry={regions.reload} />
        ) : list.length === 0 ? (
          <Notice variant="info">No region is available yet. Try again in a few minutes.</Notice>
        ) : (
          <FieldGroup>
            <ErrorNotice error={create.error} />
            <Field disabled={create.busy}>
              <Label>Name</Label>
              <Input
                required
                autoFocus
                value={name}
                placeholder="laptop"
                onChange={(event) => setName(event.currentTarget.value)}
              />
              <Description>Letters, digits, dots, dashes and underscores.</Description>
            </Field>
            {list.length > 1 && (
              <Field disabled={create.busy}>
                <Label>Region</Label>
                <Listbox value={region} onChange={setPicked}>
                  {list.map((option) => (
                    <ListboxOption key={option.slug} value={option.slug}>
                      {option.name}
                    </ListboxOption>
                  ))}
                </Listbox>
              </Field>
            )}
            <Field disabled={create.busy}>
              <Label>Expires</Label>
              <Listbox value={expiresIn} onChange={setExpiresIn}>
                <ListboxOption value="30d">In 30 days</ListboxOption>
                <ListboxOption value="90d">In 90 days</ListboxOption>
                <ListboxOption value="365d">In a year</ListboxOption>
                <ListboxOption value="never">Never</ListboxOption>
              </Listbox>
            </Field>
          </FieldGroup>
        )}
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={create.busy}>
          Cancel
        </Button>
        <Button
          type="submit"
          loading={create.loading}
          disabled={name.trim() === '' || region === ''}
          disabledReason="Fill in required fields"
        >
          Create key
        </Button>
      </DialogActions>
    </form>
  )
}

function NewKey({ apiKey, onDone }: { apiKey: api.NewSandboxKey; onDone: () => void }) {
  const { tabs } = sandboxSnippets(apiKey.region, apiKey.token)
  const env = `export SB_API_URL=${apiKey.env.SB_API_URL}\nexport SB_API_TOKEN=${apiKey.env.SB_API_TOKEN}`
  return (
    <>
      <DialogTitle>Copy your API key</DialogTitle>
      <DialogDescription>
        {apiKey.name} works in {apiKey.region.name}
        {apiKey.expires_at ? ` until ${formatDate(apiKey.expires_at)}` : ' until you revoke it'}.
      </DialogDescription>
      <DialogBody className="space-y-6">
        <Notice variant="warning">Copy it now. It can’t be shown again.</Notice>
        <CodeBlock code={apiKey.token} label="Copy API key" />
        <div>
          <Text className="mb-2">Set these, then run an example.</Text>
          <CodeBlock code={env} label="Copy environment variables" />
        </div>
        <CodeTabs tabs={tabs} label="Examples" />
        <Text size="sm">Creating a sandbox at a limit returns 429; the SDKs retry.</Text>
      </DialogBody>
      <DialogActions>
        <Button onClick={onDone}>Done</Button>
      </DialogActions>
    </>
  )
}
