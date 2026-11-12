import { BarChart, type ChartRow } from '@harlew-dev/ui/chart'
import type * as api from '../lib/api'
import { formatDay } from '../lib/format'

const most = 5

// vCPU-hours per day, stacked by region and labelled with each region's
// name. Past five regions, the smallest fold into "Other".
export function UsageChart({ usage, regionName }: { usage: api.Usage; regionName: (slug: string) => string }) {
  const totals = new Map<string, number>()
  for (const bucket of usage.data) {
    for (const [region, value] of Object.entries(bucket.regions ?? {})) {
      totals.set(region, (totals.get(region) ?? 0) + value.vcpu_hours)
    }
  }
  const ranked = [...totals.keys()].sort((a, b) => totals.get(b)! - totals.get(a)!)
  const shown = ranked.length > most ? ranked.slice(0, most - 1) : ranked
  const other = ranked.length > most
  const series = [
    ...shown.map((region) => ({ key: region, label: regionName(region) })),
    ...(other ? [{ key: 'other', label: 'Other' }] : []),
  ]
  if (series.length === 0) series.push({ key: 'all', label: 'vCPU-hours' })

  const rows: ChartRow<string>[] = usage.data.map((bucket) => {
    const values: Record<string, number> = Object.fromEntries(series.map(({ key }) => [key, 0]))
    for (const [region, value] of Object.entries(bucket.regions ?? {})) {
      const key = shown.includes(region) ? region : 'other'
      values[key] = round((values[key] ?? 0) + value.vcpu_hours)
    }
    if (series[0]?.key === 'all') values.all = round(bucket.vcpu_hours)
    return { label: formatDay(bucket.start), values }
  })

  return (
    <BarChart
      className="mt-12"
      label="vCPU-hours per day"
      summary="vCPU-hours per day, by region."
      rows={rows}
      series={series}
      tableToggle
    />
  )
}

function round(value: number): number {
  return Math.round(value * 100) / 100
}
