import {
  Button,
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
  { value: 'closed', label: 'Closed', description: 'Only you add workspaces, with New workspace.' },
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
  // '' is no default plan or region.
  const [defaultPlan, setDefaultPlan] = useState(settings.default_plan ?? '')
  const [defaultRegion, setDefaultRegion] = useState(settings.default_region ?? '')
  // A region that isn't ready stays listed while it is the default.
  const options = [
    ...regions.map((region) => ({ slug: region.slug, name: region.name })),
    ...(settings.default_region && !regions.some((region) => region.slug === settings.default_region)
      ? [{ slug: settings.default_region, name: settings.default_region }]
      : []),
  ]

  const changes: api.SettingsUpdate = {}
  if (name.trim() !== settings.instance_name) changes.instance_name = name.trim()
  if (signup !== settings.signup) changes.signup = signup
  if (defaultPlan !== (settings.default_plan ?? '')) changes.default_plan = defaultPlan || 'none'
  if (defaultRegion !== (settings.default_region ?? '')) changes.default_region = defaultRegion || 'none'
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
      <SettingsSection title="Instance name" description="Shown to workspace members on every page.">
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
      <SettingsSection title="New workspaces" description="The plan a new workspace gets in the default region.">
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
          <Field disabled={save.busy}>
            <Label>Default region</Label>
            <Listbox value={defaultRegion} onChange={setDefaultRegion}>
              <ListboxOption value="">None</ListboxOption>
              {options.map((region) => (
                <ListboxOption key={region.slug} value={region.slug}>
                  {region.name}
                </ListboxOption>
              ))}
            </Listbox>
          </Field>
          {options.length === 0 && <Text>No region is ready yet.</Text>}
          {(defaultPlan === '' || defaultRegion === '') && (
            <Text>New workspaces get no region; they wait until you assign one.</Text>
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
