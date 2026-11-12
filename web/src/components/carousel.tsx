import { Button } from '@harlew-dev/ui'
import { useState, type KeyboardEvent, type ReactNode } from 'react'

// One slide: a placeholder image until the owner supplies the real artwork,
// a title and a short description.
export interface CarouselSlide {
  title: string
  description: ReactNode
  image: ReactNode
}

// A small carousel of guidance slides. The page owns the content and what
// Skip and the last slide's action do.
export function Carousel({
  label,
  slides,
  onSkip,
  onFinish,
  skipLabel = 'Skip',
  finishLabel = 'Go to dashboard',
}: {
  label: string
  slides: CarouselSlide[]
  onSkip: () => void
  onFinish: () => void
  skipLabel?: string
  finishLabel?: string
}) {
  const [index, setIndex] = useState(0)
  const last = index === slides.length - 1
  const go = (next: number) => setIndex(Math.max(0, Math.min(slides.length - 1, next)))
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'ArrowLeft') {
      event.preventDefault()
      go(index - 1)
    } else if (event.key === 'ArrowRight') {
      event.preventDefault()
      go(index + 1)
    }
  }

  return (
    <section
      role="region"
      aria-roledescription="carousel"
      aria-label={label}
      tabIndex={0}
      onKeyDown={onKeyDown}
      className="grid w-full grid-cols-1 gap-6 focus:outline-hidden"
    >
      <div className="overflow-hidden rounded-xl">
        <div
          className="flex transition-transform duration-300 ease-out motion-reduce:transition-none"
          style={{ transform: `translateX(-${index * 100}%)` }}
        >
          {slides.map((slide, slideIndex) => (
            <div
              key={slide.title}
              role="group"
              aria-roledescription="slide"
              aria-label={`${slideIndex + 1} of ${slides.length}`}
              aria-hidden={slideIndex !== index}
              className="w-full shrink-0"
            >
              {slide.image}
              <div className="mt-6 space-y-2">
                <h2 className="text-xl font-semibold tracking-tight text-zinc-950 dark:text-white">{slide.title}</h2>
                <p className="text-base/7 text-zinc-600 dark:text-zinc-400">{slide.description}</p>
              </div>
            </div>
          ))}
        </div>
      </div>

      <div className="flex items-center justify-between gap-4">
        <Button plain onClick={() => go(index - 1)} disabled={index === 0}>
          Previous
        </Button>
        <div className="flex items-center gap-2">
          {slides.map((slide, slideIndex) => (
            <button
              key={slide.title}
              type="button"
              aria-label={`Go to slide ${slideIndex + 1}`}
              aria-current={slideIndex === index ? 'true' : undefined}
              onClick={() => go(slideIndex)}
              className={`size-2.5 rounded-full ${
                slideIndex === index ? 'bg-zinc-900 dark:bg-white' : 'bg-zinc-300 dark:bg-zinc-600'
              }`}
            />
          ))}
        </div>
        {last ? (
          <Button onClick={onFinish}>{finishLabel}</Button>
        ) : (
          <Button onClick={() => go(index + 1)}>Next</Button>
        )}
      </div>

      <div className="text-center">
        <Button plain onClick={onSkip}>
          {skipLabel}
        </Button>
      </div>
    </section>
  )
}
