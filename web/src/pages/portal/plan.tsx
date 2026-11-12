import { Divider, PageHeader, Text } from '@harlew-dev/ui'
import * as api from '../../lib/api'
import { PlanLimits } from '../../components/plan-limits'
import { NoRegion } from '../../components/no-region'
import { useInstance } from '../../lib/instance'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'
import { usePortalOrg } from './portal-route'

export function PlanPage() {
  useDocumentTitle('Plan & Billing')
  const { org } = usePortalOrg()
  const { name } = useInstance()
  const limits = useLoad(() => api.getLimits(org.slug), [org.slug])

  return (
    <>
      <PageHeader title="Plan & Billing" />
      {!org.region ? (
        <NoRegion />
      ) : (
        <>
          <div className="mb-8">
            <div className="text-sm/6 text-zinc-500 dark:text-zinc-400">Your plan</div>
            <div className="mt-1 text-lg font-semibold text-zinc-950 dark:text-white">
              {org.region.plan.name}
            </div>
            <Text>in {org.region.name}</Text>
          </div>
          <Divider className="mb-8" soft />
          <PlanLimits limits={limits} heading={false} />
          <Text className="mt-10">To change your plan, contact {name}.</Text>
        </>
      )}
    </>
  )
}
