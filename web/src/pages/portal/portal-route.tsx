import { Button, EmptyState, ErrorNotice, ListSkeleton, Notice } from '@harlew-dev/ui'
import { Outlet, useOutletContext } from 'react-router'
import { PortalLayout } from '../../components/portal-layout'
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
  const { name } = useInstance()
  if (org.status === 'pending') {
    return (
      <Notice variant="info" className="mb-8">
        Your workspace is waiting for approval. You can look around now; API keys and sandboxes work once
        it’s approved.
      </Notice>
    )
  }
  if (org.status !== 'suspended') return null
  return (
    <Notice variant="warning" className="mb-8">
      Your workspace is suspended, so its sandboxes are stopped. Contact {name} to restore it.
    </Notice>
  )
}

// The portal's layout around its pages, with the current workspace
// (organization) loaded for them and its status banner above them. The
// workspace loads here, above the layout's page transition, so it loads once
// rather than again on every page: a page change then shows the page's own
// header straight away, with placeholders only where its data goes.
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
      <PortalLayout>
        <EmptyState
          title="You’re not in a workspace"
          description="Ask someone on your team to add you to theirs."
          action={<Button href="/docs">Read the docs</Button>}
        />
      </PortalLayout>
    )
  }
  if (loading && !data) {
    return (
      <PortalLayout>
        <ListSkeleton label="Loading your workspace" rows={4} />
      </PortalLayout>
    )
  }
  if (error.message || !data) {
    return (
      <PortalLayout>
        <ErrorNotice error={error} onRetry={reload} />
      </PortalLayout>
    )
  }

  const context: PortalOrg = {
    org: data,
    can: orgPermissions(data.role ? data : { ...data, role: current.role }, user?.instance_admin === true),
    reload,
  }
  return (
    <PortalLayout>
      <StatusBanner org={data} />
      <Outlet context={context} />
    </PortalLayout>
  )
}

export function usePortalOrg(): PortalOrg {
  return useOutletContext<PortalOrg>()
}
