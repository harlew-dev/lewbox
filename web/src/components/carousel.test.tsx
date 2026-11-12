import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { Carousel } from './carousel'

const slides = [
  { title: 'One', description: 'First', image: <div>one</div> },
  { title: 'Two', description: 'Second', image: <div>two</div> },
  { title: 'Three', description: 'Third', image: <div>three</div> },
]

function renderCarousel() {
  const onSkip = vi.fn()
  const onFinish = vi.fn()
  render(<Carousel label="Tour" slides={slides} onSkip={onSkip} onFinish={onFinish} />)
  return { onSkip, onFinish }
}

describe('Carousel', () => {
  it('moves with the buttons, dots and arrow keys', () => {
    renderCarousel()
    const next = screen.getByRole('button', { name: 'Next' })
    const previous = screen.getByRole('button', { name: 'Previous' })
    expect(previous).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Go to slide 1' })).toHaveAttribute('aria-current', 'true')

    fireEvent.click(next)
    expect(screen.getByRole('button', { name: 'Go to slide 2' })).toHaveAttribute('aria-current', 'true')
    expect(previous).toBeEnabled()

    fireEvent.click(screen.getByRole('button', { name: 'Go to slide 3' }))
    expect(screen.getByRole('button', { name: 'Go to slide 3' })).toHaveAttribute('aria-current', 'true')
    expect(screen.getByRole('button', { name: 'Go to dashboard' })).toBeInTheDocument()

    const carousel = screen.getByRole('region', { name: 'Tour' })
    fireEvent.keyDown(carousel, { key: 'ArrowLeft' })
    expect(screen.getByRole('button', { name: 'Go to slide 2' })).toHaveAttribute('aria-current', 'true')
    fireEvent.keyDown(carousel, { key: 'ArrowRight' })
    expect(screen.getByRole('button', { name: 'Go to slide 3' })).toHaveAttribute('aria-current', 'true')
  })

  it('labels the slides and finishes or skips', () => {
    const { onSkip, onFinish } = renderCarousel()
    const groups = screen.getAllByRole('group', { hidden: true })
    expect(groups[0]).toHaveAttribute('aria-roledescription', 'slide')
    expect(groups[0]).toHaveAttribute('aria-label', '1 of 3')
    expect(groups[2]).toHaveAttribute('aria-label', '3 of 3')

    fireEvent.click(screen.getByRole('button', { name: 'Skip' }))
    expect(onSkip).toHaveBeenCalledOnce()

    fireEvent.click(screen.getByRole('button', { name: 'Go to slide 3' }))
    fireEvent.click(screen.getByRole('button', { name: 'Go to dashboard' }))
    expect(onFinish).toHaveBeenCalledOnce()
  })
})
