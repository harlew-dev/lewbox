import { cx } from '@harlew-dev/ui'

// "1 Configure → 2 Install commands", for the two-step server dialogs.
export function StepIndicator({ labels, current }: { labels: string[]; current: number }) {
  return (
    <ol aria-label="Steps" className="mt-4 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
      {labels.map((label, index) => (
        <li
          key={label}
          aria-current={index === current ? 'step' : undefined}
          className={cx(
            'flex items-center gap-2',
            index === current ? 'font-semibold text-zinc-950 dark:text-white' : 'text-zinc-500 dark:text-zinc-400',
          )}
        >
          <span
            className={cx(
              'flex size-5 items-center justify-center rounded-full text-xs',
              index === current
                ? 'bg-zinc-950 text-white dark:bg-white dark:text-zinc-950'
                : 'bg-zinc-200 text-zinc-700 dark:bg-zinc-700 dark:text-zinc-200',
            )}
          >
            {index + 1}
          </span>
          {label}
          {index < labels.length - 1 && <span aria-hidden="true">→</span>}
        </li>
      ))}
    </ol>
  )
}
