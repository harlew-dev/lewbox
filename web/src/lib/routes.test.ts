import { describe, expect, it } from 'vitest'
import { routes } from './routes'

describe('routes', () => {
  it('builds the portal and settings pages', () => {
    expect(routes.home()).toBe('/')
    expect(routes.sandboxes()).toBe('/sandboxes')
    expect(routes.sandboxes('snapshots')).toBe('/sandboxes?tab=snapshots')
    expect(routes.usage()).toBe('/usage')
    expect(routes.usage('7')).toBe('/usage?days=7')
    expect(routes.settings.general()).toBe('/settings')
    expect(routes.settings.members()).toBe('/settings/members')
  })

  it('builds the console pages and leaves defaults out', () => {
    expect(routes.admin.overview()).toBe('/admin')
    expect(routes.admin.workspaces()).toBe('/admin/workspaces')
    expect(routes.admin.workspaces('pending')).toBe('/admin/workspaces?status=pending')
    expect(routes.admin.workspaces('setup')).toBe('/admin/workspaces?tab=setup')
    expect(routes.admin.workspace('acme labs')).toBe('/admin/workspaces/acme%20labs')
    expect(routes.admin.servers()).toBe('/admin/servers')
    expect(routes.admin.servers('nodes', 'eu west')).toBe('/admin/servers?tab=nodes&cluster=eu+west')
    expect(routes.admin.cluster('eu/west')).toBe('/admin/servers/clusters/eu%2Fwest')
    expect(routes.admin.settings()).toBe('/admin/settings')
    expect(routes.admin.settings('dns')).toBe('/admin/settings?tab=dns')
  })

  it('builds auth, docs and setup links', () => {
    expect(routes.login()).toBe('/login')
    expect(routes.login('/setup/abc')).toBe('/login?next=%2Fsetup%2Fabc')
    expect(routes.setup('a b')).toBe('/setup/a%20b')
    expect(routes.docs()).toBe('/docs')
    expect(routes.docs('sandboxes/archived-sandboxes')).toBe('/docs/sandboxes/archived-sandboxes')
    expect(routes.platformDocs('clusters/add-nodes')).toBe('/platform/clusters/add-nodes')
  })
})
