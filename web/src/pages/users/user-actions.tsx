import {
  Button,
  ConfirmationDialog,
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
  Dropdown,
  DropdownButton,
  DropdownDivider,
  DropdownItem,
  DropdownMenu,
  ErrorNotice,
  NewPasswordFields,
  PasswordField,
  toast,
  useAction,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import {
  CheckCircleIcon,
  EllipsisHorizontalIcon,
  KeyIcon,
  NoSymbolIcon,
  TrashIcon,
} from '@heroicons/react/16/solid'
import { useState, type FormEvent } from 'react'
import * as api from '../../lib/api'
import { errorField, failureMessage } from '../../lib/use-load'

function newPasswordProblems(
  password: string,
  confirmation: string,
  current: string,
): Record<string, string> {
  if (current === '') return { currentPassword: 'Enter your password.' }
  if (password.length < 12) return { newPassword: 'Use at least 12 characters.' }
  if (password !== confirmation) return { confirmation: 'The passwords do not match.' }
  return {}
}

// The ⋯ menu for one account (lewbox user enable|disable|set-password|remove)
// and the dialogs it opens.
export function UserActions({
  user,
  onChanged,
  onRemoved,
}: {
  user: api.User
  onChanged: () => void
  onRemoved: () => void
}) {
  const [passwordOpen, setPasswordOpen] = useState(false)
  const [removeOpen, setRemoveOpen] = useState(false)
  const toggle = useAction(
    async () => {
      await api.setUserDisabled(user.id, !user.disabled)
      toast.success(user.disabled ? 'Account enabled.' : 'Account disabled. Its sessions have ended.')
      onChanged()
    },
    {
      onError: (failure) => {
        toast.error(failureMessage(failure))
        return true
      },
    },
  )

  return (
    <>
      <Dropdown>
        <DropdownButton as={Button} plain aria-label={`Actions for ${user.email}`}>
          <EllipsisHorizontalIcon />
        </DropdownButton>
        <DropdownMenu anchor="bottom end">
          <DropdownItem disabled={toggle.busy} onClick={() => void toggle.run()}>
            {user.disabled ? <CheckCircleIcon data-slot="icon" /> : <NoSymbolIcon data-slot="icon" />}
            {user.disabled ? 'Enable account' : 'Disable account'}
          </DropdownItem>
          <DropdownItem onClick={() => setPasswordOpen(true)}>
            <KeyIcon data-slot="icon" />
            Set password
          </DropdownItem>
          <DropdownDivider />
          <DropdownItem variant="danger" onClick={() => setRemoveOpen(true)}>
            <TrashIcon data-slot="icon" />
            Remove account
          </DropdownItem>
        </DropdownMenu>
      </Dropdown>

      <SetPasswordDialog
        user={passwordOpen ? user : null}
        onClose={() => setPasswordOpen(false)}
        onChanged={onChanged}
      />
      <ConfirmationDialog
        open={removeOpen}
        onClose={() => setRemoveOpen(false)}
        title={`Remove ${user.email}?`}
        description="The account, its sessions and its workspace memberships are deleted. This cannot be undone."
        confirm="Remove account"
        variant="danger"
        onConfirm={async () => {
          await api.removeUser(user.id)
        }}
        onConfirmed={() => {
          toast.success('Account removed.')
          onRemoved()
        }}
      />
    </>
  )
}

function SetPasswordDialog({
  user,
  onClose,
  onChanged,
}: {
  user: api.User | null
  onClose: () => void
  onChanged: () => void
}) {
  const count = useOpenCount(user !== null)
  return (
    <Dialog open={user !== null} onClose={onClose} size="md">
      {user && <SetPassword key={count} user={user} onClose={onClose} onChanged={onChanged} />}
    </Dialog>
  )
}

function SetPassword({
  user,
  onClose,
  onChanged,
}: {
  user: api.User
  onClose: () => void
  onChanged: () => void
}) {
  const [currentPassword, setCurrentPassword] = useState('')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [errors, setErrors] = useState<Record<string, string>>({})
  const save = useAction(
    () => api.setPassword(user.id, { password, current_password: currentPassword }),
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
    const problems = newPasswordProblems(password, confirmation, currentPassword)
    setErrors(problems)
    if (Object.keys(problems).length > 0) return
    const result = await save.run()
    if (result.ok) {
      setCurrentPassword('')
      toast.success('Password changed. Their sessions have ended.')
      onChanged()
      onClose()
    }
  }

  return (
    <form onSubmit={submit}>
      <DialogTitle>Set a new password</DialogTitle>
      <DialogDescription>For {user.email}. Their signed-in sessions end.</DialogDescription>
      <ErrorNotice error={save.error} className="mt-6" />
      <DialogBody>
        <div className="space-y-6">
          <NewPasswordFields
            password={password}
            confirmation={confirmation}
            onPasswordChange={setPassword}
            onConfirmationChange={setConfirmation}
            errors={errors}
            disabled={save.busy}
            requirements={['At least 12 characters.']}
          />
          <PasswordField
            label="Your password"
            description="To confirm it's you."
            autoComplete="current-password"
            value={currentPassword}
            onChange={setCurrentPassword}
            error={errors.currentPassword}
            disabled={save.busy}
            autoFocus={false}
            required={false}
          />
        </div>
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={save.busy}>
          Cancel
        </Button>
        <Button type="submit" loading={save.loading}>
          Set password
        </Button>
      </DialogActions>
    </form>
  )
}
