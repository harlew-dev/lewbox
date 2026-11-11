import { UptimeBar } from '@harlew-dev/ui'
import type * as api from '../lib/api'

// The API's uptime days as the library's bar.
export function Uptime({ label, uptime, days }: { label: string; uptime: number | null; days: api.UptimeDay[] }) {
  return (
    <UptimeBar
      label={label}
      uptime={uptime}
      days={days.map((day) => ({ date: day.date, uptime: day.uptime, downMinutes: day.down_minutes }))}
    />
  )
}
