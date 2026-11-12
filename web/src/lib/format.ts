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

// A whole or fractional amount, at most `digits` decimals: 1,234.5.
export function formatNumber(value: number, digits = 2): string {
  return value.toLocaleString('en-US', { maximumFractionDigits: digits })
}

// Memory in MiB as the larger unit people think in: 512 MB, 4 GB, 1.5 GB.
export function formatMemory(mb: number): string {
  if (mb < 1024) return `${formatNumber(mb, 0)} MB`
  return `${formatNumber(mb / 1024, 1)} GB`
}

// A limit, or "Unlimited" for null.
export function formatLimit(value: number | null, format: (value: number) => string = (v) => formatNumber(v)): string {
  return value === null ? 'Unlimited' : format(value)
}

// How long ago something started, as "3h 12m" or "2d 4h".
export function formatAge(value: string, now = Date.now()): string {
  const minutes = Math.max(0, Math.floor((now - new Date(value).getTime()) / 60000))
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ${minutes % 60}m`
  return `${Math.floor(hours / 24)}d ${hours % 24}h`
}

// A short date for a chart's axis: "Nov 3", in UTC like the buckets.
export function formatDay(value: string): string {
  return new Date(value).toLocaleDateString('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' })
}

// The start of the UTC day `days - 1` days ago, so a range of 7 days ends today.
export function daysAgo(days: number, now = new Date()): string {
  return new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() - (days - 1))).toISOString()
}

// The start of this calendar month, UTC.
export function monthStart(now = new Date()): string {
  return new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1)).toISOString()
}
