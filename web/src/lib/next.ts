// Only allow the sign-in page to send a visitor where they were going when
// the destination is a path on this site. Anything protocol-relative,
// absolute, backslash-escaped or the login page itself falls back to the
// dashboard.
export function safeNext(value: string | null | undefined): string {
  if (!value) return '/'
  if (!value.startsWith('/')) return '/'
  if (value.startsWith('//')) return '/'
  if (value.includes('\\')) return '/'
  // Browsers strip tabs and newlines from URLs, so "/\t/evil.com" would
  // become the protocol-relative "//evil.com". Refuse any control character.
  // eslint-disable-next-line no-control-regex
  if (/[\u0000-\u001f\u007f]/.test(value)) return '/'
  if (value === '/login' || value.startsWith('/login?') || value.startsWith('/login#')) {
    return '/'
  }
  return value
}
