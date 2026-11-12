// Thin typed client for the orchestrator's /v0 API. The browser sends the
// session cookie; credentials are same-origin and the API speaks JSON.

export interface User {
  id: string
  email: string
  instance_admin: boolean
  disabled: boolean
  created_at: string
}

export interface MeOrganization {
  id: string
  slug: string
  name: string
  role: Role
  status: OrgStatus
}

export interface Me {
  user?: User
  organizations?: MeOrganization[]
  via: string
}

export type SignupMode = 'open' | 'approval' | 'closed'

// Public: what the sign-in page and the portal need before anyone signs in.
export interface Instance {
  name: string
  signup: SignupMode
}

export interface Settings {
  instance_name: string
  signup: SignupMode
  // A plan slug, or null for none.
  default_plan: string | null
  // Cluster slugs new sign-ups get the default plan in.
  default_regions: string[]
}

// Fields that are set change. default_plan "none" clears it; an empty
// default_regions clears them.
export interface SettingsUpdate {
  instance_name?: string
  signup?: SignupMode
  default_plan?: string
  default_regions?: string[]
}

// null is unlimited.
export interface Limits {
  sandboxes: number | null
  vcpus: number | null
  memory_mb: number | null
}

export interface Plan {
  id: string
  slug: string
  name: string
  max_sandboxes: number | null
  max_vcpus: number | null
  max_memory_mb: number | null
  // Subscriptions on this plan, across every workspace and region.
  subscriptions: number
  default: boolean
  created_at: string
}

export interface PlanInput {
  slug?: string
  name?: string
  max_sandboxes?: number | null
  max_vcpus?: number | null
  max_memory_mb?: number | null
}

export interface PlanRef {
  slug: string
  name: string
}

// A cluster as customers see it. For a customer, plan and limits are
// their workspace's subscription there.
export interface Region {
  slug: string
  name: string
  endpoint_url: string
  runtime: string
  status?: string
  plan?: PlanRef
  limits?: Limits
}

// A workspace's plan in one region. overrides is only sent to instance
// admins; a null override uses the plan's limit.
export interface Subscription {
  region: Region
  plan: PlanRef
  limits: Limits
  overrides?: Limits
  created_at: string
}

// An absent override is unchanged (null on create); null uses the plan's.
export interface SubscriptionInput {
  plan: string
  max_sandboxes?: number | null
  max_vcpus?: number | null
  max_memory_mb?: number | null
}

export interface Snapshot {
  name: string
  region: string
  source_sandbox_id: string
  image: string
  created_at: string
  cpu: number
  memory_mb: number
  disk_gb: number
  push_state: string
  push_error?: string
}

export interface SnapshotList {
  data: Snapshot[]
  // Nodes that didn't answer, so the list may be missing some.
  partial_nodes: string[]
}

export interface SandboxKey {
  id: string
  name: string
  region: Region
  token_hint: string
  created_by?: string
  created_at: string
  expires_at: string | null
  revoked_at: string | null
  expired: boolean
}

export interface NewSandboxKey extends SandboxKey {
  token: string
  env: { SB_API_URL: string; SB_API_TOKEN: string }
  e2b_env?: { E2B_API_URL: string; E2B_SANDBOX_URL: string; E2B_API_KEY: string }
}

export type SandboxStatus = 'creating' | 'started' | 'stopped' | 'destroyed' | 'error'

export interface Sandbox {
  id: string
  region: string
  organization?: { slug: string; name: string }
  name: string
  image: string
  status: SandboxStatus | string
  status_reason: string
  runtime: string
  cpu: number
  memory_mb: number
  disk_gb: number
  node?: string
  created_at: string
  last_seen_at: string
}

export interface SandboxFilter {
  region?: string
  status?: string
  org?: string
}

export interface UsageAmounts {
  sandbox_hours: number
  vcpu_hours: number
  memory_gib_hours: number
}

export interface UsageBucket extends UsageAmounts {
  start: string
  peak_sandboxes: number
  regions: Record<string, { vcpu_hours: number }>
}

export interface Usage {
  from: string
  to: string
  granularity: 'hour' | 'day'
  totals: UsageAmounts
  data: UsageBucket[]
}

export interface LimitsUsed {
  sandboxes: number
  vcpus: number
  memory_mb: number
}

// One subscribed region: its plan's limits and what is in use now.
export interface RegionLimits {
  slug: string
  name: string
  plan: PlanRef
  limits: Limits
  used: LimitsUsed
  quota: { max_sandboxes: number; max_cpu: number; max_memory_mb: number } | null
  blocked: boolean
  pushed_at: string | null
  error: string
}

export interface LimitsReport {
  regions: RegionLimits[]
}

export interface UptimeDay {
  date: string
  uptime: number | null
  samples?: number
  down_minutes?: number
}

export interface ClusterHealth {
  cluster: { uptime: number | null; days: UptimeDay[] }
  nodes: { name: string; uptime: number | null; days: UptimeDay[] }[]
}

export type MetricsRange = '24h' | '7d' | '30d'

export interface NodeMetric {
  start: string
  sandboxes: number
  cpu_cores: number
  cpu_reserved: number
  memory_total_mb: number
  memory_used_mb: number
  disk_free_gb: number
  alive: number
}

export interface ClusterMetrics {
  range: MetricsRange
  bucket_seconds: number
  nodes: { name: string; data: NodeMetric[] }[]
}

export interface Overview {
  customers: { total: number; pending: number; suspended: number }
  sandboxes: { running: number; stopped: number }
  capacity: {
    nodes_alive: number
    nodes: number
    cpu_cores: number
    cpu_reserved: number
    memory_total_mb: number
    memory_used_mb: number
  }
  clusters: { slug: string; name: string; status: string; uptime: number | null; days: UptimeDay[] }[]
  running: { start: string; sandboxes: number }[]
  top_customers: { slug: string; name: string; vcpu_hours: number }[]
}

export interface SessionResponse {
  expires_at: string
  user: User
}

export interface Cluster {
  id: string
  slug: string
  name: string
  admin_url: string
  endpoint_url: string
  runtime: string
  admin_token_last4: string
  status: string
  status_detail?: string
  version?: string
  nodes: number
  checked_at?: string
  created_at: string
}

export interface Check {
  name: string
  ok: boolean
  fatal?: boolean
  detail?: string
}

export interface ClusterDetail extends Cluster {
  checks: Check[]
  node_list: Node[]
}

export interface Node {
  id: string
  name: string
  url: string
  created_at: string
  cluster_id?: string
  cluster?: string
  role?: string
  version?: string
  alive?: boolean
  last_seen_at?: string
}

export interface JoinToken {
  cluster_id: string
  cluster: string
  runtime: string
  version: string
  role: string
  token_role: string
  token: string
  ca_cert_hash: string
  join_urls: string[]
  expires_at: string
  command: string
}

export type Role = 'owner' | 'admin' | 'member' | 'viewer'

export type OrgStatus = 'pending' | 'active' | 'suspended'

export interface Organization {
  id: string
  slug: string
  name: string
  status: OrgStatus
  status_reason?: string
  team?: string
  // The subscribed regions, with the plan in each.
  regions: { slug: string; name: string; plan: PlanRef }[]
  // Instance admins only.
  vcpu_hours_30d?: number
  members: number
  role?: Role
  created_at: string
}

export interface OrganizationUpdate {
  name?: string
  status?: OrgStatus
}

export interface OrganizationList {
  data: Organization[]
}

export interface Member {
  id: string
  organization_id: string
  user_id: string
  email: string
  role: Role
  created_at: string
}

export interface MemberList {
  data: Member[]
}

export interface AdminKey {
  id: string
  organization_id: string
  name: string
  prefix: string
  last4: string
  status: 'active' | 'expired' | 'revoked'
  created_by?: string
  created_at: string
  last_used_at?: string
  expires_at?: string
  revoked_at?: string
}

export interface NewAdminKey extends AdminKey {
  token: string
}

export interface AdminKeyList {
  data: AdminKey[]
}

export interface ClusterList {
  data: Cluster[]
}

export interface NodeList {
  data: Node[]
}

export interface UserList {
  data: User[]
}

export interface DocsItem {
  title: string
  path: string
  description?: string
  external?: string
}

export interface DocsGroup {
  title: string
  items: DocsItem[]
}

export interface DocsNav {
  groups: DocsGroup[]
}

export interface PageLink {
  title: string
  path: string
}

export interface DocsPage {
  path: string
  title: string
  description?: string
  markdown: string
  prev?: PageLink
  next?: PageLink
}

interface ErrorBody {
  error?: {
    code?: unknown
    message?: unknown
    details?: unknown
  }
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details?: unknown

  constructor(status: number, code: string, message: string, details?: unknown) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.details = details
  }
}

const UNAUTHORIZED_EVENT = 'lewbox:unauthorized'

// Requests that are expected to answer 401 without meaning "your session
// ended", so they never trigger a redirect.
const AUTH_PATHS = new Set([
  '/v0/auth/session',
  '/v0/auth/signup',
  '/v0/auth/login',
  '/v0/auth/logout',
  '/v0/me',
])

export function onUnauthorized(handler: () => void): () => void {
  if (typeof window === 'undefined') return () => {}
  window.addEventListener(UNAUTHORIZED_EVENT, handler)
  return () => window.removeEventListener(UNAUTHORIZED_EVENT, handler)
}

function parseError(status: number, body: string): ApiError {
  let parsed: ErrorBody | undefined
  try {
    parsed = JSON.parse(body) as ErrorBody
  } catch {
    parsed = undefined
  }
  const error = parsed?.error
  const message =
    typeof error?.message === 'string' && error.message !== ''
      ? error.message
      : `Request failed with status ${status}.`
  const code = typeof error?.code === 'string' ? error.code : 'unknown'
  return new ApiError(status, code, message, error?.details)
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: unknown
  authRedirect?: boolean
  // The organization a member or admin-key route acts on.
  org?: string
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  const init: RequestInit = {
    method: options.method ?? 'GET',
    credentials: 'same-origin',
    headers,
  }
  if (options.org) headers['Lewbox-Organization'] = options.org
  if (options.body !== undefined) {
    init.body = JSON.stringify(options.body)
    headers['Content-Type'] = 'application/json'
  }

  let response: Response
  try {
    response = await fetch(path, init)
  } catch {
    throw new ApiError(0, 'unavailable', 'Could not reach the Lewbox server.')
  }

  const text = await response.text()
  if (!response.ok) {
    const error = parseError(response.status, text)
    if (response.status === 401 && options.authRedirect !== false && !AUTH_PATHS.has(path)) {
      window.dispatchEvent(new Event(UNAUTHORIZED_EVENT))
    }
    throw error
  }

  if (text === '') return undefined as T
  try {
    return JSON.parse(text) as T
  } catch {
    throw new ApiError(response.status, 'invalid_response', 'The server sent an invalid response.')
  }
}

function ref(value: string): string {
  return encodeURIComponent(value)
}

// "?a=1&b=2" from the set values, or "".
function query(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') search.set(key, String(value))
  }
  const text = search.toString()
  return text ? `?${text}` : ''
}

export function me(): Promise<Me> {
  return request<Me>('/v0/me', { authRedirect: false })
}

export function signIn(email: string, password: string): Promise<SessionResponse> {
  return request<SessionResponse>('/v0/auth/session', {
    method: 'POST',
    body: { email, password },
    authRedirect: false,
  })
}

export function signUp(input: {
  email: string
  password: string
  organization: string
}): Promise<SessionResponse> {
  return request<SessionResponse>('/v0/auth/signup', {
    method: 'POST',
    body: input,
    authRedirect: false,
  })
}

export function instance(): Promise<Instance> {
  return request<Instance>('/v0/instance', { authRedirect: false })
}

export function signOut(): Promise<{ ok: boolean }> {
  return request<{ ok: boolean }>('/v0/auth/logout', {
    method: 'POST',
    authRedirect: false,
  })
}

export function listClusters(): Promise<ClusterList> {
  return request<ClusterList>('/v0/clusters')
}

export function getCluster(clusterRef: string): Promise<ClusterDetail> {
  return request<ClusterDetail>(`/v0/clusters/${ref(clusterRef)}`)
}

export function checkCluster(clusterRef: string): Promise<ClusterDetail> {
  return request<ClusterDetail>(`/v0/clusters/${ref(clusterRef)}/check`, {
    method: 'POST',
  })
}

export function removeCluster(clusterRef: string): Promise<Cluster> {
  return request<Cluster>(`/v0/clusters/${ref(clusterRef)}`, {
    method: 'DELETE',
  })
}

export function listNodes(cluster?: string): Promise<NodeList> {
  const query = cluster ? `?cluster=${ref(cluster)}` : ''
  return request<NodeList>(`/v0/nodes${query}`)
}

export function removeNode(nodeRef: string, force: boolean): Promise<Node> {
  const query = force ? '?force=true' : ''
  return request<Node>(`/v0/nodes/${ref(nodeRef)}${query}`, {
    method: 'DELETE',
  })
}

export function listUsers(): Promise<UserList> {
  return request<UserList>('/v0/users')
}

export function createUser(input: {
  email: string
  password: string
  instance_admin: boolean
}): Promise<User> {
  return request<User>('/v0/users', { method: 'POST', body: input })
}

export function setUserDisabled(userRef: string, disabled: boolean): Promise<User> {
  return request<User>(`/v0/users/${ref(userRef)}/${disabled ? 'disable' : 'enable'}`, {
    method: 'POST',
  })
}

export function removeUser(userRef: string): Promise<User> {
  return request<User>(`/v0/users/${ref(userRef)}`, { method: 'DELETE' })
}

export function setPassword(userRef: string, password: string): Promise<User> {
  return request<User>(`/v0/users/${ref(userRef)}/password`, {
    method: 'PUT',
    body: { password },
  })
}

export function getUser(userRef: string): Promise<User> {
  return request<User>(`/v0/users/${ref(userRef)}`)
}

export function addCluster(input: {
  slug: string
  name: string
  admin_url: string
  endpoint_url: string
  runtime: string
  admin_token: string
}): Promise<ClusterDetail> {
  return request<ClusterDetail>('/v0/clusters', { method: 'POST', body: input })
}

export function createJoinToken(
  clusterRef: string,
  input: { role: string; ttl: string },
): Promise<JoinToken> {
  return request<JoinToken>(`/v0/clusters/${ref(clusterRef)}/join-tokens`, {
    method: 'POST',
    body: input,
  })
}

export function addNode(input: { name: string; url: string }): Promise<Node> {
  return request<Node>('/v0/nodes', { method: 'POST', body: input })
}

export function listOrganizations(filter: { user?: string; status?: OrgStatus } = {}): Promise<OrganizationList> {
  return request<OrganizationList>(`/v0/organizations${query(filter)}`)
}

export function getOrganization(orgRef: string): Promise<Organization> {
  return request<Organization>(`/v0/organizations/${ref(orgRef)}`)
}

export function createOrganization(input: {
  slug: string
  name: string
  owner: string
}): Promise<Organization> {
  return request<Organization>('/v0/organizations', { method: 'POST', body: input })
}

export function updateOrganization(orgRef: string, input: OrganizationUpdate): Promise<Organization> {
  return request<Organization>(`/v0/organizations/${ref(orgRef)}`, {
    method: 'PATCH',
    body: input,
  })
}

export function listMembers(org: string): Promise<MemberList> {
  return request<MemberList>('/v0/members', { org })
}

export function addMember(org: string, input: { email: string; role: Role }): Promise<Member> {
  return request<Member>('/v0/members', { method: 'POST', body: input, org })
}

export function setMemberRole(org: string, memberRef: string, role: Role): Promise<Member> {
  return request<Member>(`/v0/members/${ref(memberRef)}`, {
    method: 'PATCH',
    body: { role },
    org,
  })
}

export function removeMember(org: string, memberRef: string): Promise<Member> {
  return request<Member>(`/v0/members/${ref(memberRef)}`, { method: 'DELETE', org })
}

export function listAdminKeys(org: string): Promise<AdminKeyList> {
  return request<AdminKeyList>('/v0/admin-keys', { org })
}

export function createAdminKey(
  org: string,
  input: { name: string; expires_in: string },
): Promise<NewAdminKey> {
  return request<NewAdminKey>('/v0/admin-keys', { method: 'POST', body: input, org })
}

export function revokeAdminKey(org: string, keyRef: string): Promise<AdminKey> {
  return request<AdminKey>(`/v0/admin-keys/${ref(keyRef)}`, { method: 'DELETE', org })
}

export function getSettings(): Promise<Settings> {
  return request<Settings>('/v0/settings')
}

export function updateSettings(input: SettingsUpdate): Promise<Settings> {
  return request<Settings>('/v0/settings', { method: 'PATCH', body: input })
}

export function listPlans(): Promise<{ data: Plan[] }> {
  return request<{ data: Plan[] }>('/v0/plans')
}

export function createPlan(input: PlanInput): Promise<Plan> {
  return request<Plan>('/v0/plans', { method: 'POST', body: input })
}

export function updatePlan(planRef: string, input: PlanInput): Promise<Plan> {
  return request<Plan>(`/v0/plans/${ref(planRef)}`, { method: 'PATCH', body: input })
}

export function deletePlan(planRef: string): Promise<Plan> {
  return request<Plan>(`/v0/plans/${ref(planRef)}`, { method: 'DELETE' })
}

// Instance admins get every ready region. Anyone else gets the regions
// `org` is subscribed to, with the plan and limits there.
export function listRegions(org?: string): Promise<{ data: Region[] }> {
  return request<{ data: Region[] }>('/v0/regions', { org })
}

export function listSubscriptions(org: string): Promise<{ data: Subscription[] }> {
  return request<{ data: Subscription[] }>('/v0/subscriptions', { org })
}

export function setSubscription(org: string, region: string, input: SubscriptionInput): Promise<Subscription> {
  return request<Subscription>(`/v0/subscriptions/${ref(region)}`, { method: 'PUT', body: input, org })
}

// force removes it even when the region's keys can't be revoked (a 502).
export function deleteSubscription(org: string, region: string, force = false): Promise<Subscription> {
  return request<Subscription>(`/v0/subscriptions/${ref(region)}${query({ force: force ? 'true' : undefined })}`, {
    method: 'DELETE',
    org,
  })
}

// Without a region, every subscribed region.
export function listSnapshots(org: string, region?: string): Promise<SnapshotList> {
  return request<SnapshotList>(`/v0/snapshots${query({ region })}`, { org })
}

export function deleteSnapshot(org: string, snapshot: Pick<Snapshot, 'name' | 'region'>): Promise<Snapshot> {
  return request<Snapshot>(`/v0/snapshots/${ref(snapshot.name)}${query({ region: snapshot.region })}`, {
    method: 'DELETE',
    org,
  })
}

export function listSandboxKeys(org: string): Promise<{ data: SandboxKey[] }> {
  return request<{ data: SandboxKey[] }>('/v0/sandbox-keys', { org })
}

export function createSandboxKey(
  org: string,
  input: { name: string; region: string; expires_in: string },
): Promise<NewSandboxKey> {
  return request<NewSandboxKey>('/v0/sandbox-keys', { method: 'POST', body: input, org })
}

export function revokeSandboxKey(org: string, keyRef: string): Promise<SandboxKey> {
  return request<SandboxKey>(`/v0/sandbox-keys/${ref(keyRef)}`, { method: 'DELETE', org })
}

// The organization goes in the header and, for instance admins who see
// every organization, in ?org= too. Without one an admin sees the fleet.
export function listSandboxes(filter: SandboxFilter = {}): Promise<{ data: Sandbox[] }> {
  return request<{ data: Sandbox[] }>(`/v0/sandboxes${query({ ...filter })}`, { org: filter.org })
}

export function stopSandbox(sandbox: Pick<Sandbox, 'id' | 'region'>, org?: string): Promise<Sandbox> {
  return request<Sandbox>(`/v0/sandboxes/${ref(sandbox.id)}/stop${query({ region: sandbox.region })}`, {
    method: 'POST',
    org,
  })
}

export function deleteSandbox(sandbox: Pick<Sandbox, 'id' | 'region'>, org?: string): Promise<Sandbox> {
  return request<Sandbox>(`/v0/sandboxes/${ref(sandbox.id)}${query({ region: sandbox.region })}`, {
    method: 'DELETE',
    org,
  })
}

export function getUsage(filter: {
  from?: string
  to?: string
  granularity?: 'hour' | 'day'
  org?: string
}): Promise<Usage> {
  return request<Usage>(`/v0/usage${query(filter)}`, { org: filter.org })
}

export function getLimits(org: string): Promise<LimitsReport> {
  return request<LimitsReport>('/v0/limits', { org })
}

export function getOverview(): Promise<Overview> {
  return request<Overview>('/v0/overview')
}

export function clusterHealth(clusterRef: string, days = 30): Promise<ClusterHealth> {
  return request<ClusterHealth>(`/v0/clusters/${ref(clusterRef)}/health${query({ days })}`)
}

export function clusterMetrics(clusterRef: string, range: MetricsRange): Promise<ClusterMetrics> {
  return request<ClusterMetrics>(`/v0/clusters/${ref(clusterRef)}/metrics${query({ range })}`)
}

export function docsNav(): Promise<DocsNav> {
  return request<DocsNav>('/v0/docs')
}

export function docsPage(path: string): Promise<DocsPage> {
  const clean = path.replace(/^\/+|\/+$/g, '')
  const suffix = clean
    .split('/')
    .map((segment) => encodeURIComponent(segment))
    .join('/')
  return request<DocsPage>(`/v0/docs/${suffix}`)
}
