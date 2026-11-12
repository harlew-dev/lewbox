import { Field, FieldGroup, Input, Label, PasswordField } from '@harlew-dev/ui'

// The email and password fields of every sign-up form, with the same labels,
// autocomplete hints and browser validation.
export function SignupFields({
  email,
  password,
  onEmail,
  onPassword,
  disabled = false,
  autoFocus = false,
}: {
  email: string
  password: string
  onEmail: (value: string) => void
  onPassword: (value: string) => void
  disabled?: boolean
  autoFocus?: boolean
}) {
  return (
    <FieldGroup>
      <Field disabled={disabled}>
        <Label>Email</Label>
        <Input
          type="email"
          name="email"
          autoComplete="email"
          autoFocus={autoFocus}
          required
          value={email}
          onChange={(event) => onEmail(event.currentTarget.value)}
        />
      </Field>
      <PasswordField
        label="Password"
        autoComplete="new-password"
        value={password}
        onChange={onPassword}
        disabled={disabled}
        autoFocus={false}
      />
    </FieldGroup>
  )
}
