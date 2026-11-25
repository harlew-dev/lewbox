// Every page URL in one place. Components use these builders, never their own
// string concatenation, so a rename touches one module and the route registry
// stays the only list of pages.

function pathWithQuery(path: string, query?: Record<string, string | undefined>): string {
  if (!query) return path
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value) params.set(key, value)
  }
  const text = params.toString()
  return text ? `${path}?${text}` : path
}

function under(base: string, path = ''): string {
  const clean = path.replace(/^\/+/, '')
  return clean ? `${base}/${clean}` : base
}

export type AdminSettingsTab = 'general' | 'dns' | 'storage'
export type AdminWorkspacesTab = 'all' | 'pending' | 'suspended' | 'setup'
export type AdminServersTab = 'clusters' | 'nodes'
export type PortalSandboxesTab = 'sandboxes' | 'snapshots'
export type UsageDays = '7' | '30' | '90'

export const routes = {
  home: () => '/',
  login: (next?: string) =>
    next ? `/login?next=${encodeURIComponent(next)}` : '/login',
  signup: () => '/signup',
  setup: (token: string) => `/setup/${encodeURIComponent(token)}`,
  docs: (path = '') => under('/docs', path),
  platformDocs: (path = '') => under('/platform', path),
  onboarding: () => '/onboarding',
  gettingStarted: () => '/onboarding/getting-started',
  account: () => '/account',

  sandboxes: (tab?: PortalSandboxesTab) =>
    pathWithQuery('/sandboxes', { tab: tab === 'snapshots' ? 'snapshots' : undefined }),
  snapshots: () => '/snapshots',
  keys: () => '/keys',
  usage: (days?: UsageDays) => pathWithQuery('/usage', { days: days === '30' ? undefined : days }),
  plan: () => '/plan',
  team: () => '/team',

  settings: {
    general: () => '/settings',
    members: () => '/settings/members',
    apiKeys: () => '/settings/api-keys',
  },

  admin: {
    overview: () => '/admin',
    workspaces: (tab?: AdminWorkspacesTab) =>
      tab === 'pending' || tab === 'suspended'
        ? pathWithQuery('/admin/workspaces', { status: tab })
        : pathWithQuery('/admin/workspaces', { tab: tab === 'setup' ? tab : undefined }),
    workspace: (slug: string) => `/admin/workspaces/${encodeURIComponent(slug)}`,
    customers: () => '/admin/customers',
    customer: (slug: string) => `/admin/customers/${encodeURIComponent(slug)}`,
    sandboxes: () => '/admin/sandboxes',
    snapshots: () => '/admin/snapshots',
    plans: () => '/admin/plans',
    events: () => '/admin/events',
    servers: (tab?: AdminServersTab, cluster?: string) =>
      pathWithQuery('/admin/servers', {
        tab: tab === 'nodes' ? 'nodes' : undefined,
        cluster: tab === 'nodes' ? cluster : undefined,
      }),
    cluster: (ref: string) => `/admin/servers/clusters/${encodeURIComponent(ref)}`,
    users: () => '/admin/users',
    user: (ref: string) => `/admin/users/${encodeURIComponent(ref)}`,
    settings: (tab?: AdminSettingsTab) =>
      pathWithQuery('/admin/settings', { tab: tab === 'general' ? undefined : tab }),
  },
} as const
