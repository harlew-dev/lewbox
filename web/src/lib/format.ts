export function relativeTime(value?: string): string {
  if (!value) return 'Never'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return 'Unknown'
  const seconds = (date.getTime() - Date.now()) / 1000
  const absolute = Math.abs(seconds)
  const formatter = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })

  if (absolute < 45) return 'Just now'
  if (absolute < 90) return formatter.format(Math.round(seconds / 60), 'minute')
  if (absolute < 3600) return formatter.format(Math.round(seconds / 60), 'minute')
  if (absolute < 86400) return formatter.format(Math.round(seconds / 3600), 'hour')
  if (absolute < 86400 * 30) return formatter.format(Math.round(seconds / 86400), 'day')
  if (absolute < 86400 * 365) return formatter.format(Math.round(seconds / (86400 * 30)), 'month')
  return formatter.format(Math.round(seconds / (86400 * 365)), 'year')
}

export function formatDate(value?: string): string {
  if (!value) return 'Never'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return 'Unknown'
  return date.toLocaleString(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  })
}
