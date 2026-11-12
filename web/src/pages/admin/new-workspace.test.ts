import { describe, expect, it } from 'vitest'
import { generatePassword } from './new-workspace'

describe('generatePassword', () => {
  it('is 20 letters and digits, different each time', () => {
    const a = generatePassword()
    expect(a).toMatch(/^[A-Za-z0-9]{20}$/)
    expect(generatePassword()).not.toBe(a)
  })
})
