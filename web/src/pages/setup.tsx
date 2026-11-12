import {
  Button,
  CenteredLayout,
  ErrorNotice,
  Heading,
  Text,
  TextButton,
  TextLink,
  useAction,
} from '@harlew-dev/ui'
import { useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router'
import { BrandLogo } from '../components/brand-logo'
import { Spinner } from '../components/loading'
import { SignupFields } from '../components/signup-fields'
import { ApiError } from '../lib/api'
import * as api from '../lib/api'
import { useAuth } from '../lib/auth'
import { useInstance } from '../lib/instance'
import { useDocumentTitle } from '../lib/use-document-title'
import { failureMessage, useLoad } from '../lib/use-load'

// The public page behind an operator's setup link: register (even when
// sign-up is closed) and go on to create a workspace.
export function SetupPage() {
  useDocumentTitle('Set up your account')
  const { token = '' } = useParams()
  const { status, user, signUp, signOut } = useAuth()
  const instance = useInstance()
  const navigate = useNavigate()
  const info = useLoad(() => api.setupLinkInfo(token), [token])
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [signingOut, setSigningOut] = useState(false)
  const onboarding = `/onboarding?setup=${encodeURIComponent(token)}`

  async function signOutNow() {
    if (signingOut) return
    setSigningOut(true)
    try {
      await signOut()
    } finally {
      setSigningOut(false)
    }
  }

  const signup = useAction(
    () => signUp({ email, password, setup_token: token }),
    { toError: (failure) => ({ message: failureMessage(failure) }) },
  )

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await signup.run()
    if (result.ok) navigate(onboarding)
  }

  return (
    <CenteredLayout>
      <div className="grid w-full max-w-sm grid-cols-1 gap-8">
        <BrandLogo href="/login" />
        {info.loading && !info.data ? (
          <Spinner />
        ) : info.failure instanceof ApiError && info.failure.status === 404 ? (
          <div className="space-y-2">
            <Heading>Set up your account</Heading>
            <Text>
              This setup link is invalid or has expired. Ask {instance.name} for a new one.
            </Text>
          </div>
        ) : info.error.message || !info.data ? (
          <ErrorNotice error={info.error} onRetry={info.reload} />
        ) : (
          <>
            <div className="space-y-2">
              <Heading>Set up your account</Heading>
              <Text>
                Join {info.data.instance_name} on the {info.data.plan.name} plan in {info.data.region.name}.
              </Text>
            </div>

            {status === 'authenticated' ? (
              <>
                <Button className="w-full" onClick={() => navigate(onboarding)}>
                  Continue as {user?.email}
                </Button>
                <Text className="text-center">
                  Not you?{' '}
                  <TextButton onClick={() => void signOutNow()} disabled={signingOut}>
                    Sign out
                  </TextButton>
                </Text>
              </>
            ) : (
              <>
                <ErrorNotice error={signup.error} />
                <form className="grid grid-cols-1 gap-8" onSubmit={submit}>
                  <SignupFields
                    email={email}
                    password={password}
                    onEmail={setEmail}
                    onPassword={setPassword}
                    disabled={signup.loading}
                    autoFocus
                  />
                  <Button type="submit" className="w-full" loading={signup.loading}>
                    Create account
                  </Button>
                </form>
                <Text className="text-center">
                  Already have an account?{' '}
                  <TextLink href={`/login?next=${encodeURIComponent(`/setup/${token}`)}`}>Sign in</TextLink>
                </Text>
              </>
            )}
          </>
        )}
      </div>
    </CenteredLayout>
  )
}
