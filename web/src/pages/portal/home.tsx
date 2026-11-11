import {
  Button,
  CodeBlock,
  CodeTabs,
  ErrorNotice,
  ListSkeleton,
  Notice,
  PageHeader,
  Stat,
  Step,
  Steps,
  Subheading,
  Text,
} from '@harlew-dev/ui'
import { useState } from 'react'
import * as api from '../../lib/api'
import { LimitMeters } from '../../components/limit-meters'
import { formatMoney, formatNumber, monthStart } from '../../lib/format'
import { useInstance } from '../../lib/instance'
import { sandboxSnippets } from '../../lib/snippets'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'
import { CreateKeyDialog } from './create-key-dialog'
import { usePortalOrg } from './portal-route'

export function HomePage() {
  useDocumentTitle('Home')
  const { org } = usePortalOrg()
  const usage = useLoad(() => api.getUsage({ org: org.slug, from: monthStart() }), [org.slug])
  const sandboxes = useLoad(() => api.listSandboxes({ org: org.slug }), [org.slug])
  // Once the dashboard is opened it stays: a new sandbox while getting
  // started turns the last step green rather than swapping the page.
  const [opened, setOpened] = useState(false)

  if (!opened) {
    if ((usage.loading && !usage.data) || (sandboxes.loading && !sandboxes.data)) {
      return <ListSkeleton label="Loading" rows={4} />
    }
    if (sandboxes.error.message) return <ErrorNotice error={sandboxes.error} onRetry={sandboxes.reload} />
    const ran = (sandboxes.data?.data.length ?? 0) > 0 || (usage.data?.totals.sandbox_hours ?? 0) > 0
    if (!ran) {
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

  return <Dashboard usage={usage.data} running={sandboxes.data?.data ?? []} />
}

function GetStarted({ onDone }: { onDone: () => void }) {
  const { org, can } = usePortalOrg()
  const keys = useLoad(() => api.listSandboxKeys(org.slug), [org.slug])
  const regions = useLoad(() => api.listRegions(), [])
  const sandboxes = useLoad(() => api.listSandboxes({ org: org.slug }), [org.slug], { every: 5_000 })
  const [createOpen, setCreateOpen] = useState(false)
  const [created, setCreated] = useState<api.NewSandboxKey | null>(null)

  const live = (keys.data?.data ?? []).filter((key) => !key.revoked_at && !key.expired)
  const key = created ?? live[0] ?? null
  const region = key?.region ?? regions.data?.data[0] ?? null
  const snippets = region ? sandboxSnippets(region, created?.token) : null
  const first = (sandboxes.data?.data.length ?? 0) > 0

  return (
    <>
      <PageHeader title="Get started" description="Run your first sandbox in three steps." />
      <Steps aria-label="Get started" className="max-w-3xl">
        <Step
          title="Create an API key"
          description={key ? `${key.name} works in ${key.region.name}.` : 'A key works in one region.'}
        >
          {!key && (
            <Button onClick={() => setCreateOpen(true)} disabled={!can.use} disabledReason="Your account isn’t active yet">
              Create API key
            </Button>
          )}
        </Step>
        <Step title="Copy the environment variables and an example">
          {snippets ? (
            <div className="space-y-4">
              <CodeBlock
                code={snippets.env}
                label="Copy environment variables"
              />
              {!created && <Text size="sm">Use your key’s secret for SB_API_TOKEN; it was shown when you created it.</Text>}
              <CodeTabs tabs={snippets.tabs} label="Examples" />
            </div>
          ) : (
            <Text>No region is available yet.</Text>
          )}
        </Step>
        <Step title={first ? 'Your first sandbox is running' : 'Waiting for your first sandbox…'}>
          {first ? (
            <div className="space-y-4">
              <Notice variant="success">It’s on the Sandboxes page, with every one you start next.</Notice>
              <Button onClick={onDone}>Open your dashboard</Button>
            </div>
          ) : (
            <Text>Run the example. This updates by itself.</Text>
          )}
        </Step>
      </Steps>
      <CreateKeyDialog
        open={createOpen}
        org={org.slug}
        onClose={() => setCreateOpen(false)}
        onCreated={(value) => {
          setCreated(value)
          keys.refresh()
        }}
      />
    </>
  )
}

function Dashboard({ usage, running }: { usage: api.Usage | null; running: api.Sandbox[] }) {
  const { org } = usePortalOrg()
  const { billing, currency } = useInstance()
  const limits = useLoad(() => api.getLimits(org.slug), [org.slug])
  const credits = useLoad(
    () => (billing !== 'off' ? api.getCredits(org.slug) : Promise.resolve(null)),
    [org.slug, billing],
  )
  const count = running.filter((sandbox) => sandbox.status === 'started').length
  const limit = org.limits.sandboxes

  return (
    <>
      <PageHeader title={org.name} description={org.plan ? `${org.plan.name} plan` : undefined} />
      <div className="grid gap-8 sm:grid-cols-3">
        <Stat title="Running sandboxes" value={formatNumber(count, 0)} href="/sandboxes">
          {limit !== null && <span>of {formatNumber(limit, 0)} allowed</span>}
        </Stat>
        <Stat title="vCPU-hours this month" value={usage ? formatNumber(usage.totals.vcpu_hours, 1) : undefined} href="/usage">
          {usage && <span>{formatNumber(usage.totals.memory_gib_hours, 1)} GiB-hours of memory</span>}
        </Stat>
        {billing !== 'off' ? (
          <Stat
            title="Credits"
            value={credits.data ? formatMoney(credits.data.balance_micros, credits.data.currency || currency) : undefined}
            href="/billing"
          >
            {credits.data && (
              <span>{formatMoney(credits.data.month_spend_micros, credits.data.currency || currency)} spent this month</span>
            )}
          </Stat>
        ) : (
          <Stat title="Sandbox-hours this month" value={usage ? formatNumber(usage.totals.sandbox_hours, 1) : undefined} href="/usage" />
        )}
      </div>

      <Subheading className="mt-14 mb-6">Limits</Subheading>
      {limits.data ? (
        <LimitMeters report={limits.data} />
      ) : limits.error.message ? (
        <ErrorNotice error={limits.error} onRetry={limits.reload} />
      ) : (
        <ListSkeleton label="Loading limits" rows={1} />
      )}
    </>
  )
}
