import { Button, EmptyState, ErrorNotice, ListSkeleton } from '@harlew-dev/ui'
import { Outlet, useOutletContext, useParams } from 'react-router'
import * as api from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { orgPermissions, useOrganizations } from '../../lib/organizations'
import { useLoad } from '../../lib/use-load'

export interface OrganizationPage {
  org: api.Organization
  // Reload after a change (a rename, a status change, a member count).
  reload: () => void
  can: ReturnType<typeof orgPermissions>
}

// Loads /organizations/:org for the pages under it.
export function OrganizationRoute() {
  const { org: slug = '' } = useParams()
  const { user } = useAuth()
  const organizations = useOrganizations()
  const { data, error, failure, loading, reload } = useLoad(() => api.getOrganization(slug), [slug])

  if (loading && !data) return <ListSkeleton label="Loading the organization" rows={4} />
  if (failure instanceof api.ApiError && failure.status === 404) {
    return (
      <EmptyState
        title="Organization not found"
        description={`There is no organization ${slug} that you belong to.`}
        action={<Button href="/organizations">All organizations</Button>}
      />
    )
  }
  if (error.message || !data) return <ErrorNotice error={error} onRetry={reload} />

  const context: OrganizationPage = {
    org: data,
    reload: () => {
      reload()
      organizations.reload()
    },
    can: orgPermissions(data, user?.instance_admin === true),
  }
  return <Outlet context={context} />
}

export function useOrganizationPage(): OrganizationPage {
  return useOutletContext<OrganizationPage>()
}
