import { describe, expect, it } from 'vitest'
import { formatClock, minuteBucket } from './timestamps'

describe('minuteBucket', () => {
  // Aligned to a minute boundary so the within-minute window is unambiguous.
  const base = 28_333_333 * 60_000

  it('returns the same value for two timestamps in the same minute', () => {
    expect(minuteBucket(base)).toBe(minuteBucket(base + 59_999))
  })

  it('differs across a minute boundary', () => {
    expect(minuteBucket(base + 60_000)).toBe(minuteBucket(base) + 1)
    expect(minuteBucket(base + 60_000)).not.toBe(minuteBucket(base))
  })
})

describe('formatClock', () => {
  it('returns a non-empty string', () => {
    expect(formatClock(1_700_000_000_000).length).toBeGreaterThan(0)
  })

  it('contains an HH:MM clock without coupling to locale', () => {
    // Locale decides 12h/24h and any AM/PM suffix; only assert the clock shape.
    expect(formatClock(1_700_000_000_000)).toMatch(/\d{1,2}:\d{2}/)
  })
})
