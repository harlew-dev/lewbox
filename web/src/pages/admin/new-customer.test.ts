import { describe, expect, it } from 'vitest'
import { generatePassword, workspaceSlug } from './new-customer'

describe('workspaceSlug', () => {
  it('keeps lowercase letters, digits and single dashes, at most 40, then a suffix', () => {
    expect(workspaceSlug('Acme Corp!', 'ab12')).toBe('acme-corp-ab12')
    expect(workspaceSlug('  --Été 2026--  ', 'zz00')).toBe('t-2026-zz00')
    expect(workspaceSlug('x'.repeat(50), 'q1w2')).toBe(`${'x'.repeat(40)}-q1w2`)
    expect(workspaceSlug('a'.repeat(39) + ' b', 'q1w2')).toBe(`${'a'.repeat(39)}-q1w2`)
    expect(workspaceSlug('!!!', 'abcd')).toBe('workspace-abcd')
  })

  it('adds 4 random lowercase letters or digits by default', () => {
    expect(workspaceSlug('Acme')).toMatch(/^acme-[a-z0-9]{4}$/)
  })
})

describe('generatePassword', () => {
  it('is 20 letters and digits, different each time', () => {
    const a = generatePassword()
    expect(a).toMatch(/^[A-Za-z0-9]{20}$/)
    expect(generatePassword()).not.toBe(a)
  })
})
