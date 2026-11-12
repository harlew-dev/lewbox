import {
  Button,
  CodeBlock,
  CodeTabs,
  ErrorNotice,
  ListSkeleton,
  PageHeader,
  Stat,
  Step,
  Steps,
  Text,
} from '@harlew-dev/ui'
import { useState } from 'react'
import * as api from '../../lib/api'
import { RegionLimits } from '../../components/limit-meters'
import { NoRegion } from '../../components/no-region'
import { daysAgo, defaultUsageDays, formatNumber } from '../../lib/format'
import { sandboxSnippets } from '../../lib/snippets'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad, type LoadState } from '../../lib/use-load'
import { CreateKeyDialog } from './create-key-dialog'
import { usePortalOrg } from './portal-route'

export function HomePage() {
  useDocumentTitle('Home')
  const { org } = usePortalOrg()
  if (org.regions.length === 0) {
    return (
      <>
        <PageHeader title={org.name} />
        <NoRegion />
      </>
    )
  }
  return <Home />
}

// Home reads this many days of daily usage: any use in them means the
// workspace is past getting started.
const historyDays = 90

function Home() {
  const { org } = usePortalOrg()
  const usage = useLoad(
    () => api.getUsage({ org: org.slug, from: daysAgo(historyDays), granularity: 'day' }),
    [org.slug],
  )
  const sandboxes = useLoad(() => api.listSandboxes({ org: org.slug }), [org.slug])
  // Once the dashboard is opened it stays: a new sandbox while getting
  // started turns the last step green rather than swapping the page.
  const [opened, setOpened] = useState(false)

  if (!opened) {
    if ((usage.loading && !usage.data) || (sandboxes.loading && !sandboxes.data)) {
      return <ListSkeleton label="Loading" rows={4} />
    }
    if (sandboxes.error.message) return <ErrorNotice error={sandboxes.error} onRetry={sandboxes.reload} />
    if ((sandboxes.data?.data.length ?? 0) === 0) {
      if (usage.error.message) return <ErrorNotice error={usage.error} onRetry={usage.reload} />
      if ((usage.data?.totals.sandbox_hours ?? 0) === 0) {
        return (
          <GetStarted
            onDone={() => {
              setOpened(true)
              usage.reload()
              sandboxes.reload()
            }}
          />
        )
      }
    }
  }

  return <Dashboard usage={usage} running={sandboxes.data?.data ?? []} />
}

function GetStarted({ onDone }: { onDone: () => void }) {
  const { org, can } = usePortalOrg()
  const keys = useLoad(() => api.listSandboxKeys(org.slug), [org.slug])
  const regions = useLoad(() => api.listRegions(org.slug), [org.slug])
  const sandboxes = useLoad(() => api.listSandboxes({ org: org.slug }), [org.slug], { every: 5_000 })
  const [createOpen, setCreateOpen] = useState(false)
  const [created, setCreated] = useState<api.NewSandboxKey | null>(null)

  const live = (keys.data?.data ?? []).filter((key) => !key.revoked_at && !key.expired)
  const key = created ?? live[0] ?? null
  const subscribed = new Set(org.regions.map((option) => option.slug))
  const region = key?.region ?? regions.data?.data.find((option) => subscribed.has(option.slug)) ?? null
  const snippets = region ? sandboxSnippets(region, created ?? undefined) : null
  const first = (sandboxes.data?.data.length ?? 0) > 0

  return (
    <>
      <PageHeader title="Get started" />
      <Steps aria-label="Get started" className="max-w-3xl">
        <Step
          title="Create an API key"
          description={key ? `${key.name} works in ${key.region.name}.` : undefined}
        >
          {!key && (
            <Button
              onClick={() => setCreateOpen(true)}
              disabled={!can.use}
              disabledReason={org.status === 'active' ? 'Viewers can’t create API keys' : 'Your workspace isn’t active yet'}
            >
              Create API key
            </Button>
          )}
        </Step>
        <Step title="Copy the environment variables and an example">
          {snippets ? (
            <div className="space-y-4">
              <CodeBlock code={snippets.env} label="Copy environment variables" />
              {!created && <Text size="sm">Use your key’s secret as SB_API_TOKEN.</Text>}
              <CodeTabs tabs={snippets.tabs} label="Examples" />
            </div>
          ) : (
            <Text>No region is available yet.</Text>
          )}
        </Step>
        <Step title={first ? 'Your first sandbox is running' : 'Waiting for your first sandbox…'}>
          {first ? <Button onClick={onDone}>Open your dashboard</Button> : <Text>Run an example.</Text>}
        </Step>
      </Steps>
      <CreateKeyDialog
        open={createOpen}
        org={org}
        onClose={() => setCreateOpen(false)}
        onCreated={(value) => {
          setCreated(value)
          keys.refresh()
        }}
      />
    </>
  )
}

// The last `defaultUsageDays` of daily usage: what Usage opens with.
function recentTotals(usage: api.Usage): api.UsageAmounts {
  const from = daysAgo(defaultUsageDays)
  const totals = { sandbox_hours: 0, vcpu_hours: 0, memory_gib_hours: 0 }
  for (const bucket of usage.data) {
    if (new Date(bucket.start) < new Date(from)) continue
    totals.sandbox_hours += bucket.sandbox_hours
    totals.vcpu_hours += bucket.vcpu_hours
    totals.memory_gib_hours += bucket.memory_gib_hours
  }
  return totals
}

function Dashboard({ usage, running }: { usage: LoadState<api.Usage>; running: api.Sandbox[] }) {
  const { org } = usePortalOrg()
  const limits = useLoad(() => api.getLimits(org.slug), [org.slug])
  const count = running.filter((sandbox) => sandbox.status === 'started').length
  const totals = usage.data ? recentTotals(usage.data) : null
  const days = `(${defaultUsageDays} days)`

  return (
    <>
      <PageHeader title={org.name} />
      {usage.error.message && <ErrorNotice error={usage.error} onRetry={usage.reload} className="mb-8" />}
      <div className="grid gap-8 sm:grid-cols-3">
        <Stat title="Running sandboxes" value={formatNumber(count, 0)} href="/sandboxes" />
        <Stat title={`vCPU-hours ${days}`} value={totals ? formatNumber(totals.vcpu_hours, 1) : undefined} href="/usage">
          {totals && <span>{formatNumber(totals.memory_gib_hours, 1)} GiB-hours of memory</span>}
        </Stat>
        <Stat title={`Sandbox-hours ${days}`} value={totals ? formatNumber(totals.sandbox_hours, 1) : undefined} href="/usage" />
      </div>
      <RegionLimits limits={limits} />
    </>
  )
}
