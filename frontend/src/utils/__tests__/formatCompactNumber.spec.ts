import { describe, expect, it } from 'vitest'
import { formatCompactNumber, formatTokenCountM } from '../format'

describe('formatCompactNumber', () => {
  it('formats boundary values with K/M/B', () => {
    expect(formatCompactNumber(0)).toBe('0')
    expect(formatCompactNumber(999)).toBe('999')
    expect(formatCompactNumber(1000)).toBe('1.0K')
    expect(formatCompactNumber(999999)).toBe('1000.0K')
    expect(formatCompactNumber(1000000)).toBe('1.0M')
    expect(formatCompactNumber(1000000000)).toBe('1.0B')
  })

  it('supports disabling billion unit (requests style)', () => {
    expect(formatCompactNumber(1000000000, { allowBillions: false })).toBe('1000.0M')
  })

  it('returns 0 for nullish input', () => {
    expect(formatCompactNumber(null)).toBe('0')
    expect(formatCompactNumber(undefined)).toBe('0')
  })
})

describe('formatTokenCountM', () => {
  it('keeps large token counts in M style', () => {
    expect(formatTokenCountM(798_163_965)).toBe('798.16M')
    expect(formatTokenCountM(8_859_246_527)).toBe('8859.25M')
    expect(formatTokenCountM(52_570_000)).toBe('52.57M')
  })
})
