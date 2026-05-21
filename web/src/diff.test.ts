import { describe, expect, it } from 'vitest'
import { parseUnifiedDiff } from './diff'

describe('parseUnifiedDiff', () => {
  it('parses files, hunks, line kinds and numbers', () => {
    const raw = [
      'diff --git a/math.go b/math.go',
      'index 1111111..2222222 100644',
      '--- a/math.go',
      '+++ b/math.go',
      '@@ -1,3 +1,4 @@',
      ' package math',
      ' ',
      '-func Add(a, b int) int { return a + b }',
      '+func Add(a, b int) int { return a + b }',
      '+func Mul(a, b int) int { return a * b }',
    ].join('\n')

    const files = parseUnifiedDiff(raw)
    expect(files).toHaveLength(1)
    expect(files[0].path).toBe('math.go')
    expect(files[0].hunks).toHaveLength(1)

    const lines = files[0].hunks[0].lines
    expect(lines.map((l) => l.kind)).toEqual(['ctx', 'ctx', 'del', 'add', 'add'])

    // Context lines carry both old and new numbers; del only old, add only new.
    expect(lines[0]).toMatchObject({ kind: 'ctx', oldLine: 1, newLine: 1 })
    expect(lines[2]).toMatchObject({ kind: 'del', oldLine: 3 })
    expect(lines[2].newLine).toBeUndefined()
    expect(lines[3]).toMatchObject({ kind: 'add', newLine: 3 })
    expect(lines[3].oldLine).toBeUndefined()
    expect(lines[4]).toMatchObject({ kind: 'add', newLine: 4 })

    // The leading marker char is stripped from the rendered content.
    expect(lines[3].content).toBe('func Add(a, b int) int { return a + b }')
  })

  it('handles multiple files', () => {
    const raw = [
      'diff --git a/one.txt b/one.txt',
      '--- a/one.txt',
      '+++ b/one.txt',
      '@@ -1 +1,2 @@',
      ' a',
      '+b',
      'diff --git a/two.txt b/two.txt',
      '--- a/two.txt',
      '+++ b/two.txt',
      '@@ -0,0 +1 @@',
      '+hello',
    ].join('\n')

    const files = parseUnifiedDiff(raw)
    expect(files.map((f) => f.path)).toEqual(['one.txt', 'two.txt'])
    expect(files[1].hunks[0].lines).toEqual([{ kind: 'add', content: 'hello', newLine: 1 }])
  })

  it('returns an empty list for empty input', () => {
    expect(parseUnifiedDiff('')).toEqual([])
  })
})
