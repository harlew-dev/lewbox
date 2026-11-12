import { CenteredLayout, cx } from '@harlew-dev/ui'

// A quiet wait: a small spinner that fades in only once the wait is
// noticeable, so a fast load shows nothing at all. For the sign-in and
// onboarding pages, where a list skeleton would look like the dashboard.
export function Spinner({ label = 'Loading', className }: { label?: string; className?: string }) {
  return (
    <div
      role="status"
      aria-label={label}
      className={cx(
        'flex justify-center py-8 transition-opacity delay-300 duration-300 starting:opacity-0',
        className,
      )}
    >
      <span className="size-5 animate-spin rounded-full border-2 border-zinc-950/10 border-t-zinc-500 motion-reduce:animate-none dark:border-white/10 dark:border-t-zinc-400" />
    </div>
  )
}

// A standalone page's frame while its code or data loads: the same centred
// card the page itself will draw, so arriving at it doesn't flash a skeleton.
export function CenteredLoading({ label }: { label?: string }) {
  return (
    <CenteredLayout>
      <Spinner label={label} />
    </CenteredLayout>
  )
}
