import {
  Button,
  DescriptionList,
  DescriptionDetails,
  DescriptionTerm,
  Divider,
  ErrorNotice,
  NewPasswordFields,
  PageHeader,
  PasswordField,
  SettingsSection,
  toast,
  useAction,
} from '@harlew-dev/ui'
import { useState, type FormEvent } from 'react'
import * as api from '../lib/api'
import { useAuth } from '../lib/auth'
import { AccessTokensSection } from './access-tokens'
import { roleLabel, useOrganizations } from '../lib/organizations'
import { useDocumentTitle } from '../lib/use-document-title'
import { errorField, failureMessage } from '../lib/use-load'

function problems(password: string, confirmation: string, current: string): Record<string, string> {
  if (current === '') return { currentPassword: 'Enter your current password.' }
  if (password.length < 12) return { newPassword: 'Use at least 12 characters.' }
  if (password !== confirmation) return { confirmation: 'The passwords do not match.' }
  return {}
}

export function AccountPage() {
  useDocumentTitle('Account')
  const { user } = useAuth()
  const { current } = useOrganizations()
  const [currentPassword, setCurrentPassword] = useState('')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [errors, setErrors] = useState<Record<string, string>>({})

  const change = useAction(
    async () => {
      if (!user) return
      await api.setPassword(user.id, { password, current_password: currentPassword })
    },
    {
      onError: (failure) => {
        if (errorField(failure) !== 'current_password') return false
        setErrors((found) => ({ ...found, currentPassword: failureMessage(failure) }))
        return true
      },
      toError: (failure) => ({ message: failureMessage(failure) }),
    },
  )

  async function submit(event: FormEvent) {
    event.preventDefault()
    const found = problems(password, confirmation, currentPassword)
    setErrors(found)
    if (Object.keys(found).length > 0) return
    const result = await change.run()
    if (result.ok) {
      setCurrentPassword('')
      setPassword('')
      setConfirmation('')
      toast.success('Password changed. Other sessions were signed out.')
    }
  }

  if (!user) return null

  return (
    <div className="max-w-4xl">
      <PageHeader title="Account settings" />
      <Divider className="mb-10" />

      <SettingsSection title="Account">
        <DescriptionList>
          <DescriptionTerm>Email</DescriptionTerm>
          <DescriptionDetails>{user.email}</DescriptionDetails>
          <DescriptionTerm>Role</DescriptionTerm>
          <DescriptionDetails>
            {user.instance_admin ? 'Instance admin' : roleLabel(current?.role ?? 'member')}
          </DescriptionDetails>
          <DescriptionTerm>Sign-in method</DescriptionTerm>
          <DescriptionDetails>Password</DescriptionDetails>
        </DescriptionList>
      </SettingsSection>

      <Divider soft className="my-10" />

      <SettingsSection
        title="Password"
        description="Changing your password signs out your other browser sessions."
      >
        <form className="space-y-6" onSubmit={submit}>
          <ErrorNotice error={change.error} />
          <PasswordField
            label="Current password"
            autoComplete="current-password"
            value={currentPassword}
            onChange={setCurrentPassword}
            error={errors.currentPassword}
            disabled={change.busy}
            autoFocus={false}
            required={false}
          />
          <NewPasswordFields
            password={password}
            confirmation={confirmation}
            onPasswordChange={setPassword}
            onConfirmationChange={setConfirmation}
            errors={errors}
            disabled={change.busy}
            requirements={['At least 12 characters.']}
          />
          <div className="flex justify-end">
            <Button type="submit" loading={change.loading}>
              Change password
            </Button>
          </div>
        </form>
      </SettingsSection>

      <Divider soft className="my-10" />

      <SettingsSection
        title="Access tokens"
        description="Tokens for scripts and CI. A token acts as you, with your rights, until it expires or you revoke it."
        stacked
      >
        <AccessTokensSection instanceAdmin={user.instance_admin} />
      </SettingsSection>
    </div>
  )
}
