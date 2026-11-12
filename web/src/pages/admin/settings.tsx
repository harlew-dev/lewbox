import {
  Button,
  CheckboxField,
  Divider,
  ErrorNotice,
  Field,
  Fieldset,
  Input,
  Label,
  Legend,
  Listbox,
  ListboxOption,
  ListSkeleton,
  PageHeader,
  RadioGroup,
  SettingsSection,
  Text,
  toast,
  useAction,
} from '@harlew-dev/ui'
import { useState, type FormEvent } from 'react'
import * as api from '../../lib/api'
import { useInstance } from '../../lib/instance'
import { useDocumentTitle } from '../../lib/use-document-title'
import { failureMessage, useLoad } from '../../lib/use-load'

const signupOptions: { value: api.SignupMode; label: string; description: string }[] = [
  { value: 'closed', label: 'Closed', description: 'Only you add customers, with New customer.' },
  { value: 'approval', label: 'Approval', description: 'Anyone can sign up; their workspace waits until you approve it.' },
  { value: 'open', label: 'Open', description: 'Anyone can sign up and start at once.' },
]

export function AdminSettingsPage() {
  useDocumentTitle('Settings')
  const settings = useLoad(() => api.getSettings(), [])
  const plans = useLoad(() => api.listPlans(), [])
  const regions = useLoad(() => api.listRegions(), [])

  return (
    <div className="max-w-4xl">
      <PageHeader title="Settings" />
      <Divider className="mb-10" />
      {settings.loading && !settings.data ? (
        <ListSkeleton label="Loading settings" rows={4} />
      ) : settings.error.message || !settings.data ? (
        <ErrorNotice error={settings.error} onRetry={settings.reload} />
      ) : (
        <SettingsForm
          settings={settings.data}
          plans={plans.data?.data ?? []}
          regions={regions.data?.data ?? []}
          onSaved={settings.refresh}
        />
      )}
    </div>
  )
}

function sameRegions(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((slug) => b.includes(slug))
}

function SettingsForm({
  settings,
  plans,
  regions,
  onSaved,
}: {
  settings: api.Settings
  plans: api.Plan[]
  regions: api.Region[]
  onSaved: () => void
}) {
  const instance = useInstance()
  const [name, setName] = useState(settings.instance_name)
  const [signup, setSignup] = useState(settings.signup)
  // '' is no default plan.
  const [defaultPlan, setDefaultPlan] = useState(settings.default_plan ?? '')
  const [defaultRegions, setDefaultRegions] = useState(settings.default_regions)
  // Regions that aren't ready stay listed while they are defaults.
  const options = [
    ...regions.map((region) => ({ slug: region.slug, name: region.name })),
    ...settings.default_regions
      .filter((slug) => !regions.some((region) => region.slug === slug))
      .map((slug) => ({ slug, name: slug })),
  ]

  const changes: api.SettingsUpdate = {}
  if (name.trim() !== settings.instance_name) changes.instance_name = name.trim()
  if (signup !== settings.signup) changes.signup = signup
  if (defaultPlan !== (settings.default_plan ?? '')) changes.default_plan = defaultPlan || 'none'
  if (!sameRegions(defaultRegions, settings.default_regions)) changes.default_regions = defaultRegions
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

  function toggle(slug: string, on: boolean) {
    setDefaultRegions((before) => (on ? [...before.filter((other) => other !== slug), slug] : before.filter((other) => other !== slug)))
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
      <SettingsSection title="New sign-ups" description="The plan a new workspace gets in each default region.">
        <div className="space-y-6">
          <Field disabled={save.busy}>
            <Label>Default plan</Label>
            <Listbox value={defaultPlan} onChange={setDefaultPlan}>
              <ListboxOption value="">None</ListboxOption>
              {plans.map((plan) => (
                <ListboxOption key={plan.slug} value={plan.slug}>
                  {plan.name}
                </ListboxOption>
              ))}
            </Listbox>
          </Field>
          <Fieldset disabled={save.busy}>
            <Legend>Default regions</Legend>
            {options.length === 0 ? (
              <Text className="mt-2">No region is ready yet.</Text>
            ) : (
              <div className="mt-3 space-y-3">
                {options.map((region) => (
                  <CheckboxField
                    key={region.slug}
                    checked={defaultRegions.includes(region.slug)}
                    onChange={(on) => toggle(region.slug, on)}
                  >
                    {region.name}
                  </CheckboxField>
                ))}
              </div>
            )}
          </Fieldset>
          {(defaultPlan === '' || defaultRegions.length === 0) && (
            <Text>New sign-ups get no region; they wait until you add one.</Text>
          )}
        </div>
      </SettingsSection>

      <Divider soft className="my-10" />
      <div className="flex items-center justify-end gap-4">
        <ErrorNotice error={save.error} className="flex-1" />
        <Button
          type="submit"
          loading={save.loading}
          disabled={!changed || name.trim() === ''}
          disabledReason={name.trim() === '' ? 'Fill in required fields' : 'No changes made'}
        >
          Save settings
        </Button>
      </div>
    </form>
  )
}
