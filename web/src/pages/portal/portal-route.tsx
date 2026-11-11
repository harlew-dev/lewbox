import { Button, EmptyState, ErrorNotice, ListSkeleton, Notice, TextLink } from '@harlew-dev/ui'
import { Outlet, useOutletContext } from 'react-router'
import * as api from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { useInstance } from '../../lib/instance'
import { orgPermissions, useOrganizations, type Permissions } from '../../lib/organizations'
import { useLoad } from '../../lib/use-load'

export interface PortalOrg {
  org: api.Organization
  can: Permissions
  // Reload after a change to the organization.
  reload: () => void
}

function StatusBanner({ org }: { org: api.Organization }) {
  const { name, billing } = useInstance()
  if (org.status === 'pending') {
    return (
      <Notice variant="info" className="mb-8">
        Your account is waiting for approval. You can look around now; API keys and sandboxes work once
        it’s approved.
      </Notice>
    )
  }
  if (org.status !== 'suspended') return null
  return (
    <Notice variant="warning" className="mb-8">
      {org.status_reason === 'credits' ? (
        <>
          Your account is paused: it has run out of credits, so its sandboxes are stopped.
          {billing !== 'off' && (
            <>
              {' '}
              See <TextLink href="/billing">Billing</TextLink>.
            </>
          )}
        </>
      ) : (
        `Your account is suspended, so its sandboxes are stopped. Contact ${name} to restore it.`
      )}
    </Notice>
  )
}

// Loads the current organization for the portal's pages, with its status
// banner above them.
export function PortalRoute() {
  const { user } = useAuth()
  const { current } = useOrganizations()
  const slug = current?.slug ?? ''
  const { data, error, loading, reload } = useLoad(
    () => (slug ? api.getOrganization(slug) : Promise.resolve(null)),
    [slug],
  )

  if (!current) {
    return (
      <EmptyState
        title="You’re not in an organization"
        description="Ask someone on your team to add you to theirs."
        action={<Button href="/docs">Read the docs</Button>}
      />
    )
  }
  if (loading && !data) return <ListSkeleton label="Loading your organization" rows={4} />
  if (error.message || !data) return <ErrorNotice error={error} onRetry={reload} />

  const context: PortalOrg = {
    org: data,
    can: orgPermissions(data.role ? data : { ...data, role: current.role }, user?.instance_admin === true),
    reload,
  }
  return (
    <>
      <StatusBanner org={data} />
      <Outlet context={context} />
    </>
  )
}

export function usePortalOrg(): PortalOrg {
  return useOutletContext<PortalOrg>()
}
