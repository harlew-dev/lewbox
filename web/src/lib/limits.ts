// Limit fields in forms. An empty field is null (no limit on a plan, the
// plan's limit on a subscription); a limit is never 0.

export function limitText(value: number | null | undefined): string {
  return value == null ? '' : String(value)
}

// null for an empty field, NaN for anything but a whole number above 0.
export function parseLimit(text: string): number | null {
  if (text.trim() === '') return null
  const value = Number(text)
  return Number.isInteger(value) && value > 0 ? value : Number.NaN
}

export function validLimits(...texts: string[]): boolean {
  return texts.every((text) => !Number.isNaN(parseLimit(text)))
}

export const invalidLimitReason = 'Limits are whole numbers above 0'
