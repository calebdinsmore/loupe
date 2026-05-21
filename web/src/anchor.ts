// Content-aware comment anchoring. A comment is stored against a (path, line)
// pair, but the underlying diff shifts when the working tree is edited or a
// committed branch is amended/rebased, so the stored line can point at the
// wrong content — or vanish entirely. relocate() uses the line content captured
// at comment creation (the anchor) to find where the comment belongs now.

import type { DiffLine, FileDiff } from './diff'

// lineNumber mirrors how App.tsx identifies a diff row: the new-side number when
// present, else the old-side number. Keeping this identical keeps relocation and
// rendering in agreement about which line a comment lands on.
function lineNumber(l: DiffLine): number {
  return l.newLine ?? l.oldLine ?? 0
}

export interface Anchor {
  line: number // the line number stored with the comment
  anchorText: string // the line's content captured when the comment was created
}

// DEFAULT_WINDOW bounds how far relocate() will search from the stored line for
// a matching line. Small enough that an unrelated identical line far away won't
// hijack the comment; large enough to absorb a typical local edit.
export const DEFAULT_WINDOW = 5

// relocate returns the line number where an anchored comment should render in
// the freshly parsed file, or null if it can't be placed (orphaned/outdated).
//
// Heuristic:
//  1. If the stored line still carries the anchor text, keep it there.
//  2. Otherwise pick the nearest line within ±window whose content matches the
//     anchor exactly; ties favor the earlier (lower) line.
//  3. If nothing matches, the comment is orphaned — return null.
//
// A comment with no anchor text (legacy rows, or file-level line 0) can't be
// content-matched, so it falls back to its stored line unchanged.
export function relocate(anchor: Anchor, file: FileDiff, window = DEFAULT_WINDOW): number | null {
  // No anchor to match against (legacy rows, file-level line 0, or a missing
  // payload) — trust the stored line rather than wrongly orphaning the comment.
  if (!anchor.anchorText) return anchor.line

  const matches: number[] = []
  for (const h of file.hunks) {
    for (const l of h.lines) {
      if (l.content === anchor.anchorText) matches.push(lineNumber(l))
    }
  }
  if (matches.length === 0) return null
  if (matches.includes(anchor.line)) return anchor.line

  let best: number | null = null
  let bestDist = Infinity
  for (const ln of matches) {
    const dist = Math.abs(ln - anchor.line)
    if (dist <= window && dist < bestDist) {
      bestDist = dist
      best = ln
    }
  }
  return best
}
