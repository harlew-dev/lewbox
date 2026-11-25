// The one HTTP helper for the orchestrator's /v0 API. The browser sends the
// session cookie; credentials are same-origin and the API speaks JSON. Only
// this module calls fetch; the resource modules call request.

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
  '/v0/profile',
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

export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
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

export function ref(value: string): string {
  return encodeURIComponent(value)
}

// "?a=1&b=2" from the set values, or "".
export function query(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') search.set(key, String(value))
  }
  const text = search.toString()
  return text ? `?${text}` : ''
}

// The step a profile test failed at, from an error body's details, if any.
export function failedStep(failure: unknown): string {
  if (!(failure instanceof ApiError)) return ''
  const details = failure.details as { step?: unknown } | undefined
  return typeof details?.step === 'string' ? details.step : ''
}
