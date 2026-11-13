import { Badge } from '@harlew-dev/ui'
import * as api from '../../lib/api'
import { relativeTime } from '../../lib/format'
import { failureMessage } from '../../lib/use-load'

// The outcome of a profile's last test: ok, failed with the message, or never.
export function LastTest({ testedAt, error }: { testedAt: string | null; error: string }) {
  if (!testedAt) return <span className="text-zinc-500 dark:text-zinc-400">Never</span>
  if (error) {
    return (
      <span title={`Tested ${relativeTime(testedAt)}`}>
        <Badge color="red">Failed</Badge> <span className="whitespace-normal text-sm">{error}</span>
      </span>
    )
  }
  return (
    <span title={`Tested ${relativeTime(testedAt)}`}>
      <Badge color="lime">OK</Badge>
    </span>
  )
}

// A save error, with the step of the profile test that failed when the
// server names one.
export function saveError(failure: unknown): { message: string } {
  const step = api.failedStep(failure)
  const message = failureMessage(failure)
  return { message: step ? `${message} (step: ${step})` : message }
}

// The masked key shown in tables.
export function masked(last4: string): string {
  return last4 ? `…${last4}` : '—'
}
