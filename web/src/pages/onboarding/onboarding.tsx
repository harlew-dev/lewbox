import {
  Button,
  ErrorNotice,
  Field,
  Heading,
  Input,
  Label,
  ListSkeleton,
  Notice,
  Text,
  useAction,
} from '@harlew-dev/ui'
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Navigate, useNavigate, useSearchParams } from 'react-router'
import * as api from '../../lib/api'
import { ApiError } from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { useInstance } from '../../lib/instance'
import { useOrganizations } from '../../lib/organizations'
import { useDocumentTitle } from '../../lib/use-document-title'
import { failureMessage, useLoad } from '../../lib/use-load'
import { OnboardingChrome } from './chrome'

const setupKey = 'lewbox:setup'

function rememberedSetup(): string {
  try {
    return sessionStorage.getItem(setupKey) ?? ''
  } catch {
    return ''
  }
}

function rememberSetup(token: string) {
  try {
    sessionStorage.setItem(setupKey, token)
  } catch {
    // Not remembered; the URL still carries it for this visit.
  }
}

function forgetSetup() {
  try {
    sessionStorage.removeItem(setupKey)
  } catch {
    // Nothing to clear.
  }
}

// A signed-in account with no workspace creates its first one here.
export function OnboardingPage() {
  useDocumentTitle('New workspace')
  const { user, organizations, reloadOrganizations } = useAuth()
  const { select } = useOrganizations()
  const instance = useInstance()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const fromUrl = searchParams.get('setup') ?? ''
  const [token, setToken] = useState(() => fromUrl || rememberedSetup())
  const [staleToken, setStaleToken] = useState(false)
  const created = useRef(false)
  const [name, setName] = useState('')

  useEffect(() => {
    if (fromUrl) rememberSetup(fromUrl)
  }, [fromUrl])

  const info = useLoad(
    () => (token ? api.setupLinkInfo(token) : Promise.resolve(null)),
    [token],
  )

  // A link can expire or be used between opening the page and creating the
  // workspace. Forget it, so the form neither promises a plan nor sends it.
  useEffect(() => {
    if (token && info.failure instanceof ApiError && info.failure.status === 404) {
      forgetSetup()
      setStaleToken(true)
      setToken('')
    }
  }, [token, info.failure])

  const create = useAction(
    () =>
      api.createOrganization({
        name: name.trim(),
        ...(token ? { setup_token: token } : {}),
      }),
    { toError: (failure) => ({ message: failureMessage(failure) }) },
  )

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await create.run()
    if (!result.ok) return
    created.current = true
    await reloadOrganizations()
    select(result.value.slug)
    forgetSetup()
    navigate('/onboarding/getting-started', { replace: true })
  }

  if (organizations.length > 0 && !created.current) return <Navigate to="/" replace />

  if (!instance.loaded) {
    return (
      <OnboardingChrome>
        <ListSkeleton label="Loading" rows={3} />
      </OnboardingChrome>
    )
  }

  const closed = instance.signup === 'closed' && token === '' && user?.instance_admin !== true

  return (
    <OnboardingChrome>
      {staleToken && <Notice variant="warning">This setup link is invalid or has expired.</Notice>}
      {closed ? (
        <div className="space-y-4">
          <Heading>New workspace</Heading>
          <Text>Ask {instance.name} for a setup link to create a workspace.</Text>
        </div>
      ) : (
        <>
          <div className="space-y-2">
            <Heading>New workspace</Heading>
            <Text>
              A workspace is your team’s place in {instance.name}. Name your first one — you can create more
              later.
            </Text>
          </div>

          {info.data && (
            <Notice variant="info">
              You’ll join on the {info.data.plan.name} plan in {info.data.region.name}.
            </Notice>
          )}

          <ErrorNotice error={create.error} />

          <form className="grid grid-cols-1 gap-8" onSubmit={submit}>
            <Field disabled={create.loading}>
              <Label>Name</Label>
              <Input
                name="workspace"
                autoFocus
                required
                autoComplete="off"
                placeholder="e.g. Acme"
                value={name}
                onChange={(event) => setName(event.currentTarget.value)}
              />
            </Field>
            <Button
              type="submit"
              className="w-full"
              loading={create.loading}
              disabled={name.trim() === ''}
              disabledReason="Fill in required fields"
            >
              Create workspace
            </Button>
          </form>
        </>
      )}
    </OnboardingChrome>
  )
}
