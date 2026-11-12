import {
  Button,
  CenteredLayout,
  ErrorNotice,
  Heading,
  Text,
  TextLink,
  useAction,
} from '@harlew-dev/ui'
import { useState, type FormEvent } from 'react'
import { Navigate, useNavigate } from 'react-router'
import { BrandLogo } from '../components/brand-logo'
import { Spinner } from '../components/loading'
import { SignupFields } from '../components/signup-fields'
import { useAuth, homeFor } from '../lib/auth'
import { useInstance } from '../lib/instance'
import { useDocumentTitle } from '../lib/use-document-title'
import { failureMessage } from '../lib/use-load'

export function SignupPage() {
  useDocumentTitle('Create an account')
  const { status, user, signUp } = useAuth()
  const instance = useInstance()
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')

  const action = useAction(() => signUp({ email, password }), {
    toError: (failure) => ({ message: failureMessage(failure) }),
  })

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await action.run()
    if (result.ok) navigate('/onboarding', { replace: true })
  }

  if (status === 'authenticated' && !action.busy) return <Navigate to={homeFor(user)} replace />

  return (
    <CenteredLayout>
      <div className="grid w-full max-w-sm grid-cols-1 gap-8">
        <BrandLogo href="/login" />
        {!instance.loaded ? (
          <Spinner />
        ) : instance.signup === 'closed' ? (
          <div className="space-y-2">
            <Heading>Sign-up is closed</Heading>
            <Text>
              Ask {instance.name} for an account, or <TextLink href="/login">sign in</TextLink>.
            </Text>
          </div>
        ) : (
          <>
            <div className="space-y-2">
              <Heading>Create your account</Heading>
              {instance.signup === 'approval' && (
                <Text>New workspaces are approved before they can run sandboxes.</Text>
              )}
            </div>

            <ErrorNotice error={action.error} />

            <form className="grid grid-cols-1 gap-8" onSubmit={submit}>
              <SignupFields
                email={email}
                password={password}
                onEmail={setEmail}
                onPassword={setPassword}
                disabled={action.loading}
                autoFocus
              />
              <Button type="submit" className="w-full" loading={action.loading}>
                Create account
              </Button>
            </form>
          </>
        )}
        <Text className="text-center">
          Have an account? <TextLink href="/login">Sign in</TextLink>
        </Text>
      </div>
    </CenteredLayout>
  )
}
