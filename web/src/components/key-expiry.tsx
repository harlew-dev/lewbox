import { Field, Label, Listbox, ListboxOption } from '@harlew-dev/ui'
import type * as api from '../lib/api'

const lifetimes: { value: api.KeyLifetime; label: string }[] = [
  { value: '30d', label: 'In 30 days' },
  { value: '90d', label: 'In 90 days' },
  { value: '365d', label: 'In a year' },
  { value: 'never', label: 'Never' },
]

// When a new key expires: the same choices for sandbox and management keys.
export function KeyExpiryField({
  value,
  onChange,
  disabled,
}: {
  value: api.KeyLifetime
  onChange: (value: api.KeyLifetime) => void
  disabled?: boolean
}) {
  return (
    <Field disabled={disabled}>
      <Label>Expires</Label>
      <Listbox value={value} onChange={onChange}>
        {lifetimes.map((option) => (
          <ListboxOption key={option.value} value={option.value}>
            {option.label}
          </ListboxOption>
        ))}
      </Listbox>
    </Field>
  )
}
