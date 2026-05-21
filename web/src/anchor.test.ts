import { describe, expect, it } from 'vitest'
import { relocate, type Anchor } from './anchor'
import type { FileDiff } from './diff'

// file builds a single-hunk FileDiff from (newLine, content) pairs, treating
// every line as context so its newLine drives the row identity relocate() uses.
function file(lines: [number, string][]): FileDiff {
  return {
    path: 'x.ts',
    hunks: [
      {
        header: '@@',
        lines: lines.map(([newLine, content]) => ({ kind: 'ctx' as const, content, oldLine: newLine, newLine })),
      },
    ],
  }
}

describe('relocate', () => {
  it('keeps a comment whose stored line still matches', () => {
    const f = file([
      [10, 'const a = 1'],
      [11, 'const b = 2'],
    ])
    const anchor: Anchor = { line: 11, anchorText: 'const b = 2' }
    expect(relocate(anchor, f)).toBe(11)
  })

  it('relocates a comment when its line shifted down', () => {
    // The commented line moved from 11 to 14 (e.g. 3 lines inserted above it).
    const f = file([
      [13, 'const a = 1'],
      [14, 'const b = 2'],
      [15, 'const c = 3'],
    ])
    const anchor: Anchor = { line: 11, anchorText: 'const b = 2' }
    expect(relocate(anchor, f)).toBe(14)
  })

  it('prefers the nearest match when the line content is duplicated', () => {
    const f = file([
      [5, 'return err'],
      [9, 'return err'],
      [20, 'return err'],
    ])
    // Stored at 8: line 9 (dist 1) wins over 5 (dist 3) and 20 (out of window).
    const anchor: Anchor = { line: 8, anchorText: 'return err' }
    expect(relocate(anchor, f)).toBe(9)
  })

  it('orphans a comment whose line was deleted', () => {
    const f = file([
      [10, 'const a = 1'],
      [11, 'const c = 3'],
    ])
    // 'const b = 2' no longer exists anywhere — can't be placed.
    const anchor: Anchor = { line: 11, anchorText: 'const b = 2' }
    expect(relocate(anchor, f)).toBeNull()
  })

  it('orphans a comment when the only match is outside the search window', () => {
    const f = file([[40, 'const b = 2']])
    const anchor: Anchor = { line: 11, anchorText: 'const b = 2' }
    expect(relocate(anchor, f)).toBeNull()
    // A wide enough window can still reach it.
    expect(relocate(anchor, f, 40)).toBe(40)
  })

  it('breaks an equal-distance tie toward the lower line', () => {
    // Stored line 10 sits exactly between two matches (8 and 12); the lower one wins.
    const f = file([
      [8, 'return err'],
      [12, 'return err'],
    ])
    const anchor: Anchor = { line: 10, anchorText: 'return err' }
    expect(relocate(anchor, f)).toBe(8)
  })

  it('falls back to the stored line when there is no anchor text (legacy)', () => {
    const f = file([[11, 'const b = 2']])
    const anchor: Anchor = { line: 11, anchorText: '' }
    expect(relocate(anchor, f)).toBe(11)
  })

  it('leaves a file-level (line 0) comment on line 0', () => {
    // File-level comments carry no anchor text, so they stay put.
    const f = file([[1, 'first line']])
    const anchor: Anchor = { line: 0, anchorText: '' }
    expect(relocate(anchor, f)).toBe(0)
  })
})
