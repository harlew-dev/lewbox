import {
  Button,
  CenteredLayout,
  ErrorNotice,
  Field,
  FieldGroup,
  Heading,
  Input,
  Label,
  PasswordField,
  Text,
  TextLink,
  useAction,
} from '@harlew-dev/ui'
import { useState, type FormEvent } from 'react'
import { Navigate, useSearchParams } from 'react-router'
import { BrandLogo } from '../components/brand-logo'
import { ApiError } from '../lib/api'
import { useAuth } from '../lib/auth'
import { safeNext } from '../lib/next'
import { useDocumentTitle } from '../lib/use-document-title'

function signInMessage(failure: unknown): string {
  if (failure instanceof ApiError) {
    if (failure.status === 401) return 'Email or password is wrong.'
    return failure.message
  }
  return failure instanceof Error ? failure.message : 'Could not sign in.'
}

export function LoginPage() {
  useDocumentTitle('Sign in')
  const { status, signIn } = useAuth()
  const [searchParams] = useSearchParams()
  const next = safeNext(searchParams.get('next'))
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')

  const action = useAction(
    async () => {
      await signIn(email, password)
    },
    { toError: (failure) => ({ message: signInMessage(failure) }) },
  )

  async function submit(event: FormEvent) {
    event.preventDefault()
    await action.run()
  }

  if (status === 'authenticated') {
    return <Navigate to={next} replace />
  }

  return (
    <CenteredLayout>
      <div className="grid w-full max-w-sm grid-cols-1 gap-8">
        <BrandLogo href="/login" />
        <div className="space-y-2">
          <Heading>Sign in to Lewbox</Heading>
          <Text>Use your Lewbox account to manage your clusters.</Text>
        </div>

        <ErrorNotice error={action.error} />

        <form className="grid grid-cols-1 gap-8" onSubmit={submit}>
          <FieldGroup>
            <Field disabled={action.loading}>
              <Label>Email</Label>
              <Input
                type="email"
                name="email"
                autoComplete="email"
                autoFocus
                required
                value={email}
                onChange={(event) => setEmail(event.currentTarget.value)}
              />
            </Field>
            <PasswordField
              label="Password"
              autoComplete="current-password"
              value={password}
              onChange={setPassword}
              disabled={action.loading}
              autoFocus={false}
            />
          </FieldGroup>

          <Button type="submit" className="w-full" loading={action.loading}>
            Sign in
          </Button>
        </form>

        <Text className="text-center">
          Need help? Read the <TextLink href="/docs">documentation</TextLink>.
        </Text>
      </div>
    </CenteredLayout>
  )
}
