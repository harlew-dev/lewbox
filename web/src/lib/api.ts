// Thin typed client for the orchestrator's /v0 API. The browser sends the
// session cookie; credentials are same-origin and the API speaks JSON.

export interface User {
  id: string
  email: string
  instance_admin: boolean
  disabled: boolean
  created_at: string
}

export interface Me {
  user?: User
  via: string
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

export interface Organization {
  id: string
  slug: string
  name: string
  status: 'active' | 'suspended'
  members: number
  role?: Role
  created_at: string
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

export function listOrganizations(user?: string): Promise<OrganizationList> {
  const query = user ? `?user=${ref(user)}` : ''
  return request<OrganizationList>(`/v0/organizations${query}`)
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

export function updateOrganization(
  orgRef: string,
  input: { name?: string; status?: Organization['status'] },
): Promise<Organization> {
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
