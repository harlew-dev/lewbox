import { useState, type ReactNode } from 'react'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import type { Page } from '@harlew-dev/ui'
import { describe, expect, it, vi } from 'vitest'
import {
  FilterBar,
  type FilterDef,
  type FilterOption,
  type FilterValues,
} from './filter-bar'

const status: FilterDef = {
  key: 'status',
  label: 'Status',
  options: [
    { value: 'failed', label: 'Failed' },
    { value: 'succeeded', label: 'Succeeded' },
    { value: 'pending', label: 'Pending' },
  ],
}

const region: FilterDef = {
  key: 'region',
  label: 'Region',
  options: [
    { value: 'eu-1', label: 'eu-1' },
    { value: 'us-1', label: 'us-1' },
  ],
}

// The page owns the applied values; this harness is that page.
function Harness({
  filters,
  initial = {},
  leading,
  onApply = vi.fn(),
  onRemove = vi.fn(),
  onClear = vi.fn(),
}: {
  filters: FilterDef[]
  initial?: FilterValues
  leading?: ReactNode
  onApply?: (key: string, values: string[]) => void
  onRemove?: (key: string) => void
  onClear?: () => void
}) {
  const [values, setValues] = useState<FilterValues>(initial)
  return (
    <FilterBar
      filters={filters}
      values={values}
      leading={leading}
      onApply={(key, next) => {
        onApply(key, next)
        setValues((current) => ({ ...current, [key]: next }))
      }}
      onRemove={(key) => {
        onRemove(key)
        setValues((current) => {
          const next = { ...current }
          delete next[key]
          return next
        })
      }}
      onClear={() => {
        onClear()
        setValues({})
      }}
    />
  )
}

const workspaceNames: Record<string, string> = {
  'ws-1': 'Acme',
  'ws-2': 'Beta',
  'ws-3': 'Gamma',
}

function answer(values: string[], total = 1): Page<FilterOption> {
  return {
    data: values.map((value) => ({
      value,
      label: workspaceNames[value] ?? value,
    })),
    pages: { current: 1, total, perPage: 20, items: values.length },
  }
}

async function open(name: string | RegExp) {
  fireEvent.click(screen.getByRole('button', { name }))
  return screen.findByRole('dialog')
}

describe('FilterBar badges', () => {
  it('shows one dashed badge per filter and opens the filter in a popover', async () => {
    render(<Harness filters={[status, region]} />)

    const trigger = screen.getByRole('button', { name: 'Status' })
    expect(trigger).toHaveAttribute('aria-haspopup', 'dialog')
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
    expect(screen.getByRole('button', { name: 'Region' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Clear filters' })).toBeNull()

    fireEvent.click(trigger)
    const dialog = await screen.findByRole('dialog', { name: 'Filter by Status' })
    expect(within(dialog).getByRole('checkbox', { name: 'Failed' })).toBeInTheDocument()
    expect(trigger).toHaveAttribute('aria-expanded', 'true')
  })

  it('shows one applied value by name, truncated, with the full text in a tooltip', async () => {
    const long = 'A workspace with a very long name indeed'
    const workspace: FilterDef = {
      key: 'organization',
      label: 'Workspace',
      load: vi.fn(),
      selectedLabels: { acme: long },
    }
    render(<Harness filters={[workspace]} initial={{ organization: ['acme'] }} />)

    const trigger = screen.getByRole('button', { name: /Workspace\s+A workspace/ })
    expect(within(trigger).getByText(long)).toHaveClass('truncate')
    fireEvent.focus(trigger)
    expect(await screen.findByRole('tooltip')).toHaveTextContent(long)
  })

  it('says how many values are selected and lists them in the tooltip', async () => {
    render(
      <Harness
        filters={[status, region]}
        initial={{ status: ['failed', 'succeeded'] }}
      />,
    )

    const trigger = screen.getByRole('button', { name: /Status\s+2 selected/ })
    fireEvent.focus(trigger)
    expect(await screen.findByRole('tooltip')).toHaveTextContent('Failed, Succeeded')
  })

  it('keeps the badges in the order the list gives them', () => {
    render(
      <Harness
        filters={[status, region]}
        initial={{ region: ['eu-1'], status: ['failed'] }}
      />,
    )

    const names = screen
      .getAllByRole('button', { name: /^(Status|Region)\s/ })
      .map((button) => button.textContent)
    expect(names[0]).toContain('Status')
    expect(names[1]).toContain('Region')
  })
})

describe('removing and clearing', () => {
  it('removes one filter at once and focuses its badge, now unapplied', async () => {
    const onRemove = vi.fn()
    render(
      <Harness
        filters={[status, region]}
        initial={{ status: ['failed'], region: ['eu-1'] }}
        onRemove={onRemove}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Remove Status filter' }))

    expect(onRemove).toHaveBeenCalledWith('status')
    const badge = await screen.findByRole('button', { name: 'Status' })
    await waitFor(() => expect(badge).toHaveFocus())
    expect(screen.getByRole('button', { name: /Region\s+eu-1/ })).toBeInTheDocument()
  })

  it('shows Clear filters only while something is applied, and clears it all', async () => {
    render(
      <Harness
        filters={[status, region]}
        initial={{ status: ['failed'], region: ['eu-1'] }}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))

    expect(screen.getByRole('button', { name: 'Status' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Region' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Clear filters' })).toBeNull()
  })
})

describe('applying options', () => {
  it('applies several choices at once and reopens with them checked', async () => {
    const onApply = vi.fn()
    render(<Harness filters={[status]} onApply={onApply} />)

    let dialog = await open('Status')
    fireEvent.click(within(dialog).getByRole('checkbox', { name: 'Failed' }))
    fireEvent.click(within(dialog).getByRole('checkbox', { name: 'Succeeded' }))
    const trigger = screen.getByRole('button', { name: 'Status' })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Apply' }))

    expect(onApply).toHaveBeenCalledWith('status', ['failed', 'succeeded'])
    expect(
      await screen.findByRole('button', { name: /Status\s+2 selected/ }),
    ).toBeInTheDocument()
    await waitFor(() => expect(trigger).toHaveFocus())

    dialog = await open(/Status\s+2 selected/)
    expect(within(dialog).getByRole('checkbox', { name: 'Failed' })).toBeChecked()
    expect(within(dialog).getByRole('checkbox', { name: 'Succeeded' })).toBeChecked()
  })

  it('removes the filter when Apply is pressed with nothing selected', async () => {
    const onApply = vi.fn()
    render(
      <Harness filters={[status]} initial={{ status: ['failed'] }} onApply={onApply} />,
    )

    const dialog = await open(/Status\s+Failed/)
    fireEvent.click(within(dialog).getByRole('checkbox', { name: 'Failed' }))
    fireEvent.click(within(dialog).getByRole('button', { name: 'Apply' }))

    expect(onApply).toHaveBeenCalledWith('status', [])
    expect(await screen.findByRole('button', { name: 'Status' })).toBeInTheDocument()
  })

  it('offers radios for a single-value filter', async () => {
    const onApply = vi.fn()
    render(
      <Harness
        filters={[{ ...status, multiple: false }]}
        onApply={onApply}
      />,
    )

    const dialog = await open('Status')
    expect(within(dialog).queryByRole('checkbox')).toBeNull()
    fireEvent.click(within(dialog).getByRole('radio', { name: 'Succeeded' }))
    fireEvent.click(within(dialog).getByRole('button', { name: 'Apply' }))

    expect(onApply).toHaveBeenCalledWith('status', ['succeeded'])
  })

  it('discards changes on Escape and returns focus to the badge', async () => {
    const onApply = vi.fn()
    render(
      <Harness
        filters={[status]}
        initial={{ status: ['failed'] }}
        onApply={onApply}
      />,
    )

    const dialog = await open(/Status\s+Failed/)
    fireEvent.click(within(dialog).getByRole('checkbox', { name: 'Succeeded' }))
    fireEvent.keyDown(document, { key: 'Escape' })

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(onApply).not.toHaveBeenCalled()
    const badge = screen.getByRole('button', { name: /Status\s+Failed/ })
    await waitFor(() => expect(badge).toHaveFocus())
  })

  it('searches a fixed list longer than ten options', async () => {
    const many: FilterDef = {
      key: 'plan',
      label: 'Plan',
      options: Array.from({ length: 12 }, (_, i) => ({
        value: `plan-${i}`,
        label: `Plan ${i}`,
      })),
    }
    render(<Harness filters={[many]} />)

    const dialog = await open('Plan')
    fireEvent.change(within(dialog).getByRole('searchbox', { name: 'Search Plan' }), {
      target: { value: 'Plan 11' },
    })

    expect(
      await within(dialog).findByRole('checkbox', { name: 'Plan 11' }),
    ).toBeInTheDocument()
    await waitFor(() =>
      expect(within(dialog).queryByRole('checkbox', { name: 'Plan 0' })).toBeNull(),
    )
  })
})

describe('paged options', () => {
  const first = ['ws-1', 'ws-2']
  const second = ['ws-2', 'ws-3']

  function workspaceFilter(
    load: (query: string, page: number) => Promise<Page<FilterOption>>,
  ): FilterDef {
    return {
      key: 'organization',
      label: 'Workspace',
      load,
      selectedLabels: {
        'ws-1': 'Acme',
        'ws-2': 'Beta',
        'ws-3': 'Gamma',
      },
    }
  }

  it('loads a page at a time, de-duplicates values, and searches after a pause', async () => {
    const load = vi.fn(async (query: string, page: number) =>
      query
        ? answer(['ws-3'])
        : answer(page === 1 ? first : second, 2),
    )
    render(
      <Harness
        filters={[workspaceFilter(load)]}
        initial={{ organization: ['ws-1'] }}
      />,
    )

    // The applied value is named from selectedLabels before anything loads.
    expect(
      screen.getByRole('button', { name: /Workspace\s+Acme/ }),
    ).toBeInTheDocument()

    const dialog = await open(/Workspace\s+Acme/)
    expect(await within(dialog).findByRole('checkbox', { name: 'Beta' })).toBeInTheDocument()
    expect(within(dialog).getAllByRole('checkbox', { name: 'Acme' })).toHaveLength(1)
    expect(load).toHaveBeenLastCalledWith('', 1)

    fireEvent.click(within(dialog).getByRole('button', { name: 'Load more' }))
    expect(await within(dialog).findByRole('checkbox', { name: 'Gamma' })).toBeInTheDocument()
    expect(within(dialog).getAllByRole('checkbox', { name: 'Beta' })).toHaveLength(1)
    expect(load).toHaveBeenLastCalledWith('', 2)
    expect(within(dialog).queryByRole('button', { name: 'Load more' })).toBeNull()

    const search = within(dialog).getByRole('searchbox', { name: 'Search Workspace' })
    fireEvent.change(search, { target: { value: 'gam' } })
    await waitFor(() => expect(load).toHaveBeenLastCalledWith('gam', 1), {
      timeout: 2000,
    })
    expect(await within(dialog).findByRole('checkbox', { name: 'Gamma' })).toBeInTheDocument()
    expect(within(dialog).queryByRole('checkbox', { name: 'Beta' })).toBeNull()
  })

  it('offers a retry when loading more fails', async () => {
    let fail = true
    const load = vi.fn(async (_query: string, page: number) => {
      if (page === 2 && fail) throw new Error('offline')
      return answer(page === 1 ? first : second, 2)
    })
    render(<Harness filters={[workspaceFilter(load)]} />)

    const dialog = await open('Workspace')
    expect(await within(dialog).findByRole('checkbox', { name: 'Beta' })).toBeInTheDocument()

    fireEvent.click(within(dialog).getByRole('button', { name: 'Load more' }))
    expect(
      await within(dialog).findByRole('button', {
        name: "Couldn't load more · Retry",
      }),
    ).toBeInTheDocument()

    fail = false
    fireEvent.click(
      within(dialog).getByRole('button', { name: "Couldn't load more · Retry" }),
    )
    expect(await within(dialog).findByRole('checkbox', { name: 'Gamma' })).toBeInTheDocument()
  })
})

describe('date ranges', () => {
  const created: FilterDef = {
    key: 'created',
    label: 'Created',
    kind: 'range',
    presets: [
      { value: '24h', label: 'Last 24 hours' },
      { value: '7d', label: 'Last 7 days' },
      { value: '30d', label: 'Last 30 days' },
    ],
  }

  it('applies a preset and shows its name', async () => {
    const onApply = vi.fn()
    render(
      <Harness filters={[created]} initial={{ created: ['7d'] }} onApply={onApply} />,
    )

    expect(
      screen.getByRole('button', { name: /Created\s+Last 7 days/ }),
    ).toBeInTheDocument()

    const dialog = await open(/Created\s+Last 7 days/)
    fireEvent.click(within(dialog).getByRole('radio', { name: 'Last 24 hours' }))
    fireEvent.click(within(dialog).getByRole('button', { name: 'Apply' }))

    expect(onApply).toHaveBeenCalledWith('created', ['24h'])
    expect(
      await screen.findByRole('button', { name: /Created\s+Last 24 hours/ }),
    ).toBeInTheDocument()
  })

  it('applies a custom from date alone and reads it as From', async () => {
    const onApply = vi.fn()
    render(<Harness filters={[created]} onApply={onApply} />)

    const dialog = await open('Created')
    fireEvent.click(within(dialog).getByRole('radio', { name: 'Custom range' }))
    fireEvent.change(within(dialog).getByLabelText('From'), {
      target: { value: '2026-03-21' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Apply' }))

    expect(onApply).toHaveBeenCalledWith('created', ['2026-03-21', ''])
    expect(
      await screen.findByRole('button', { name: /Created\s+From 21 Mar 2026/ }),
    ).toBeInTheDocument()
  })

  it('shows both ends of a custom range', async () => {
    render(
      <Harness
        filters={[created]}
        initial={{ created: ['2026-03-21', '2026-03-25'] }}
      />,
    )

    expect(
      screen.getByRole('button', {
        name: /Created\s+21 Mar 2026 – 25 Mar 2026/,
      }),
    ).toBeInTheDocument()
  })

  it('shows the error under the field and applies nothing when the end is first', async () => {
    const onApply = vi.fn()
    render(<Harness filters={[created]} onApply={onApply} />)

    const dialog = await open('Created')
    fireEvent.click(within(dialog).getByRole('radio', { name: 'Custom range' }))
    fireEvent.change(within(dialog).getByLabelText('From'), {
      target: { value: '2026-03-25' },
    })
    fireEvent.change(within(dialog).getByLabelText('To'), {
      target: { value: '2026-03-21' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Apply' }))

    expect(
      within(dialog).getByText('The end comes before the start.'),
    ).toBeInTheDocument()
    expect(onApply).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('asks for a date when a custom range has neither end', async () => {
    const onApply = vi.fn()
    render(<Harness filters={[created]} onApply={onApply} />)

    const dialog = await open('Created')
    fireEvent.click(within(dialog).getByRole('radio', { name: 'Custom range' }))
    fireEvent.click(within(dialog).getByRole('button', { name: 'Apply' }))

    expect(within(dialog).getByText('Choose a date.')).toBeInTheDocument()
    expect(onApply).not.toHaveBeenCalled()
  })
})

describe('leading slot', () => {
  it('renders the page search before the badges', () => {
    render(
      <Harness
        filters={[status]}
        leading={<input type="search" aria-label="Search sandboxes" />}
      />,
    )

    expect(screen.getByRole('searchbox', { name: 'Search sandboxes' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Status' })).toBeInTheDocument()
  })
})
