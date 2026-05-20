// Minimal unified-diff parser. Good enough to render and anchor comments;
// swap in `react-diff-view` later if you want word-level/split views.

export type LineKind = 'add' | 'del' | 'ctx'

export interface DiffLine {
  kind: LineKind
  content: string
  oldLine?: number
  newLine?: number
}

export interface Hunk {
  header: string
  lines: DiffLine[]
}

export interface FileDiff {
  path: string
  hunks: Hunk[]
}

export function parseUnifiedDiff(raw: string): FileDiff[] {
  const files: FileDiff[] = []
  let file: FileDiff | null = null
  let hunk: Hunk | null = null
  let oldLn = 0
  let newLn = 0

  for (const line of raw.split('\n')) {
    if (line.startsWith('diff --git')) {
      file = { path: '', hunks: [] }
      files.push(file)
      hunk = null
    } else if (line.startsWith('+++ ')) {
      if (file) file.path = line.slice(4).replace(/^b\//, '')
    } else if (line.startsWith('--- ')) {
      // ignore the old-path marker
    } else if (line.startsWith('@@')) {
      const m = /@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(line)
      oldLn = m ? parseInt(m[1], 10) : 0
      newLn = m ? parseInt(m[2], 10) : 0
      hunk = { header: line, lines: [] }
      file?.hunks.push(hunk)
    } else if (hunk) {
      if (line.startsWith('+')) {
        hunk.lines.push({ kind: 'add', content: line.slice(1), newLine: newLn++ })
      } else if (line.startsWith('-')) {
        hunk.lines.push({ kind: 'del', content: line.slice(1), oldLine: oldLn++ })
      } else if (line.startsWith(' ')) {
        hunk.lines.push({ kind: 'ctx', content: line.slice(1), oldLine: oldLn++, newLine: newLn++ })
      }
    }
  }
  return files
}
