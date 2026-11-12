import '@testing-library/jest-dom/vitest'
import { cleanup, configure } from '@testing-library/react'
import { afterEach, vi } from 'vitest'

// Pages load lazily, so under a full parallel run the first findBy can wait
// on a chunk; one second is too tight for that.
configure({ asyncUtilTimeout: 5000 })

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

// jsdom lacks what Base UI's dialogs, menus and listboxes use.
if (!('ResizeObserver' in window)) {
  Object.defineProperty(window, 'ResizeObserver', {
    value: class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  })
}
if (!('PointerEvent' in window)) {
  class TestPointerEvent extends MouseEvent {
    pointerType: string
    constructor(type: string, init: MouseEventInit & { pointerType?: string } = {}) {
      super(type, init)
      this.pointerType = init.pointerType ?? ''
    }
  }
  Object.defineProperty(window, 'PointerEvent', { value: TestPointerEvent })
}
