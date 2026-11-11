import {
  Button,
  Description,
  Divider,
  ErrorNotice,
  Field,
  Input,
  Label,
  Listbox,
  ListboxOption,
  ListSkeleton,
  PageHeader,
  RadioGroup,
  SettingsSection,
  toast,
  useAction,
} from '@harlew-dev/ui'
import { useState, type FormEvent } from 'react'
import * as api from '../../lib/api'
import { useInstance } from '../../lib/instance'
import { useDocumentTitle } from '../../lib/use-document-title'
import { failureMessage, useLoad } from '../../lib/use-load'

const signupOptions: { value: api.SignupMode; label: string; description: string }[] = [
  { value: 'open', label: 'Open', description: 'Anyone can sign up and start at once.' },
  { value: 'approval', label: 'Approval', description: 'Anyone can sign up; you approve each customer.' },
  { value: 'closed', label: 'Closed', description: 'Only you add customers.' },
]

const billingOptions: { value: api.BillingMode; label: string; description: string }[] = [
  { value: 'off', label: 'Off', description: 'No prices anywhere.' },
  { value: 'show', label: 'Show', description: 'Customers see what they spend; nobody is stopped.' },
  { value: 'enforce', label: 'Enforce', description: 'Customers need credits; at zero they are paused.' },
]

export function AdminSettingsPage() {
  useDocumentTitle('Settings')
  const settings = useLoad(() => api.getSettings(), [])
  const plans = useLoad(() => api.listPlans(), [])

  return (
    <div className="max-w-4xl">
      <PageHeader title="Settings" />
      <Divider className="mb-10" />
      {settings.loading && !settings.data ? (
        <ListSkeleton label="Loading settings" rows={4} />
      ) : settings.error.message || !settings.data ? (
        <ErrorNotice error={settings.error} onRetry={settings.reload} />
      ) : (
        <SettingsForm settings={settings.data} plans={plans.data?.data ?? []} onSaved={settings.refresh} />
      )}
    </div>
  )
}

// A price in micro-units as the decimal people type: 50000 → "0.05".
function decimal(micros: number): string {
  return String(micros / 1e6)
}

function micros(text: string): number {
  return Math.round(Number(text || 0) * 1e6)
}

function SettingsForm({
  settings,
  plans,
  onSaved,
}: {
  settings: api.Settings
  plans: api.Plan[]
  onSaved: () => void
}) {
  const instance = useInstance()
  const [name, setName] = useState(settings.instance_name)
  const [signup, setSignup] = useState(settings.signup)
  const [defaultPlan, setDefaultPlan] = useState(settings.default_plan)
  const [billing, setBilling] = useState<api.BillingMode>(settings.billing ?? 'off')
  const [currency, setCurrency] = useState(settings.currency ?? '')
  const [vcpuPrice, setVcpuPrice] = useState(decimal(settings.price_vcpu_hour_micros ?? 0))
  const [memoryPrice, setMemoryPrice] = useState(decimal(settings.price_memory_gib_hour_micros ?? 0))
  const [signupCredits, setSignupCredits] = useState(decimal(settings.signup_credits_micros ?? 0))

  const next: api.Settings = {
    instance_name: name.trim(),
    signup,
    default_plan: defaultPlan,
    billing,
    currency: currency.trim(),
    price_vcpu_hour_micros: micros(vcpuPrice),
    price_memory_gib_hour_micros: micros(memoryPrice),
    signup_credits_micros: micros(signupCredits),
  }
  const before: api.Settings = {
    ...settings,
    billing: settings.billing ?? 'off',
    currency: settings.currency ?? '',
    price_vcpu_hour_micros: settings.price_vcpu_hour_micros ?? 0,
    price_memory_gib_hour_micros: settings.price_memory_gib_hour_micros ?? 0,
    signup_credits_micros: settings.signup_credits_micros ?? 0,
  }
  const changes = Object.fromEntries(
    Object.entries(next).filter(([key, value]) => before[key as keyof api.Settings] !== value),
  ) as Partial<api.Settings>
  const changed = Object.keys(changes).length > 0

  const save = useAction(() => api.updateSettings(changes), {
    toError: (failure) => ({ message: failureMessage(failure) }),
  })

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await save.run()
    if (result.ok) {
      toast.success('Settings saved.')
      instance.reload()
      onSaved()
    }
  }

  return (
    <form onSubmit={submit}>
      <SettingsSection title="Instance name" description="Shown to customers on every page.">
        <Field disabled={save.busy}>
          <Label className="sr-only">Instance name</Label>
          <Input required value={name} onChange={(event) => setName(event.currentTarget.value)} />
        </Field>
      </SettingsSection>

      <Divider soft className="my-10" />
      <SettingsSection title="Sign-up">
        <RadioGroup label="Sign-up" value={signup} onChange={setSignup} options={signupOptions} disabled={save.busy} />
      </SettingsSection>

      <Divider soft className="my-10" />
      <SettingsSection title="Default plan" description="New customers start on it.">
        <Field disabled={save.busy}>
          <Label className="sr-only">Default plan</Label>
          <Listbox value={defaultPlan} onChange={setDefaultPlan} placeholder="Choose a plan">
            {plans.map((plan) => (
              <ListboxOption key={plan.slug} value={plan.slug}>
                {plan.name}
              </ListboxOption>
            ))}
          </Listbox>
        </Field>
      </SettingsSection>

      <Divider soft className="my-10" />
      <SettingsSection title="Billing">
        <RadioGroup label="Billing" value={billing} onChange={setBilling} options={billingOptions} disabled={save.busy} />
      </SettingsSection>

      {billing !== 'off' && (
        <>
          <Divider soft className="my-10" />
          <SettingsSection title="Prices" description="Charged per hour of use, in the currency below.">
            <div className="grid gap-6 sm:grid-cols-2">
              <Field disabled={save.busy}>
                <Label>Per vCPU-hour</Label>
                <Input type="number" min={0} step="any" value={vcpuPrice} onChange={(event) => setVcpuPrice(event.currentTarget.value)} />
              </Field>
              <Field disabled={save.busy}>
                <Label>Per GiB-hour of memory</Label>
                <Input type="number" min={0} step="any" value={memoryPrice} onChange={(event) => setMemoryPrice(event.currentTarget.value)} />
              </Field>
              <Field disabled={save.busy}>
                <Label>Currency</Label>
                <Input value={currency} placeholder="USD" onChange={(event) => setCurrency(event.currentTarget.value)} />
                <Description>A label only, such as USD or credits.</Description>
              </Field>
              <Field disabled={save.busy}>
                <Label>Sign-up credits</Label>
                <Input type="number" min={0} step="0.01" value={signupCredits} onChange={(event) => setSignupCredits(event.currentTarget.value)} />
                <Description>Given once to each new customer.</Description>
              </Field>
            </div>
          </SettingsSection>
        </>
      )}

      <Divider soft className="my-10" />
      <div className="flex items-center justify-end gap-4">
        <ErrorNotice error={save.error} className="flex-1" />
        <Button type="submit" loading={save.loading} disabled={!changed || next.instance_name === ''} disabledReason="No changes made">
          Save settings
        </Button>
      </div>
    </form>
  )
}
