// List state lives in the URL. Parse forgivingly: a repeated key counts as
// absent, an unknown value falls back to the default, and the default is left
// out when writing, so each state has one URL and one cache key.

export function one(params: URLSearchParams, key: string): string {
  const values = params.getAll(key)
  return values.length === 1 ? (values[0] ?? '') : ''
}

export function oneOf(
  params: URLSearchParams,
  key: string,
  allowed: readonly string[],
  fallback = '',
): string {
  const value = one(params, key)
  return allowed.includes(value) ? value : fallback
}

export function withParam(params: URLSearchParams, key: string, value: string): URLSearchParams {
  const next = new URLSearchParams(params)
  if (value === '') next.delete(key)
  else next.set(key, value)
  return next
}
