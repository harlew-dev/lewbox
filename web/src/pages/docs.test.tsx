import { render, screen } from '@testing-library/react'
import { StrictMode } from 'react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DocsPage } from './docs'

beforeEach(() => {
  window.scrollTo = vi.fn()
  vi.stubGlobal(
    'fetch',
    vi.fn(async () =>
      new Response(
        JSON.stringify({
          path: 'guide',
          title: 'Guide',
          description: 'How to use it.',
          markdown: [
            '## Getting started',
            '',
            '> [!NOTE]',
            '> Read this first.',
            '',
            '## Same',
            '',
            '### Same',
            '',
            '```bash',
            'lewbox serve',
            '```',
          ].join('\n'),
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    ),
  )
})

describe('DocsPage', () => {
  it('renders headings with ids, alerts without the marker, and a table of contents', async () => {
    render(
      <MemoryRouter initialEntries={['/docs/guide']}>
        <Routes>
          <Route path="/docs/*" element={<DocsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('heading', { level: 1, name: 'Guide' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 2, name: /Getting started/ })).toHaveAttribute(
      'id',
      'getting-started',
    )
    expect(screen.getByRole('heading', { level: 3, name: /Same/ })).toHaveAttribute('id', 'same-1')
    expect(screen.getByText('Read this first.')).toBeInTheDocument()
    expect(screen.queryByText(/\[!NOTE\]/)).not.toBeInTheDocument()
    expect(screen.getByText('On this page')).toBeInTheDocument()
    expect(screen.getByText('lewbox serve')).toBeInTheDocument()
  })

  // Ids used to come from a counter advanced on each heading render, so a
  // second render (StrictMode, or any parent update) shifted or blanked them.
  it('keeps heading ids stable across re-renders', async () => {
    const tree = () => (
      <StrictMode>
        <MemoryRouter initialEntries={['/docs/guide']}>
          <Routes>
            <Route path="/docs/*" element={<DocsPage />} />
          </Routes>
        </MemoryRouter>
      </StrictMode>
    )
    const { rerender } = render(tree())
    await screen.findByRole('heading', { level: 1, name: 'Guide' })
    rerender(tree())
    expect(screen.getByRole('heading', { level: 2, name: /Getting started/ })).toHaveAttribute(
      'id',
      'getting-started',
    )
    expect(screen.getByRole('heading', { level: 2, name: /^Same/ })).toHaveAttribute('id', 'same')
    expect(screen.getByRole('heading', { level: 3, name: /Same/ })).toHaveAttribute('id', 'same-1')
  })
})
