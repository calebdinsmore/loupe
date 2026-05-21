import { describe, expect, it } from 'vitest'
import { buildTree, type TreeNode } from './filetree'

// Flattens the tree into "type:path" lines in render order so ordering and
// nesting assertions stay readable.
function flatten(nodes: TreeNode[], depth = 0, out: string[] = []): string[] {
  for (const n of nodes) {
    out.push(`${'  '.repeat(depth)}${n.type === 'dir' ? 'd' : 'f'}:${n.name}`)
    flatten(n.children, depth + 1, out)
  }
  return out
}

describe('buildTree', () => {
  it('returns an empty list for an empty changeset', () => {
    expect(buildTree([])).toEqual([])
  })

  it('ignores empty paths and stray slashes', () => {
    expect(buildTree(['', '/'])).toEqual([])
    // Leading/trailing/doubled slashes collapse rather than spawning blank nodes.
    expect(buildTree(['/a//b.go'])).toEqual([
      {
        name: 'a',
        path: 'a',
        type: 'dir',
        children: [{ name: 'b.go', path: 'a/b.go', type: 'file', children: [] }],
      },
    ])
  })

  it('represents a single top-level file', () => {
    expect(buildTree(['README.md'])).toEqual([
      { name: 'README.md', path: 'README.md', type: 'file', children: [] },
    ])
  })

  it('folds a deeply nested path into one node per segment', () => {
    const tree = buildTree(['internal/server/web_dist/app.go'])
    expect(flatten(tree)).toEqual([
      'd:internal',
      '  d:server',
      '    d:web_dist',
      '      f:app.go',
    ])

    // Directory nodes carry the cumulative prefix; the leaf carries the full path.
    let node = tree[0]
    expect(node.path).toBe('internal')
    node = node.children[0]
    expect(node.path).toBe('internal/server')
    node = node.children[0]
    expect(node.path).toBe('internal/server/web_dist')
    expect(node.children[0].path).toBe('internal/server/web_dist/app.go')
  })

  it('merges shared directory prefixes across many files', () => {
    const tree = buildTree([
      'web/src/App.tsx',
      'web/src/diff.ts',
      'web/package.json',
      'cmd/loupe/main.go',
    ])
    expect(flatten(tree)).toEqual([
      'd:cmd',
      '  d:loupe',
      '    f:main.go',
      'd:web',
      '  d:src',
      '    f:App.tsx',
      '    f:diff.ts',
      '  f:package.json',
    ])
  })

  it('orders directories before files, each alphabetically', () => {
    const tree = buildTree([
      'zebra.txt',
      'alpha.txt',
      'src/main.go',
      'docs/readme.md',
    ])
    expect(flatten(tree)).toEqual([
      'd:docs',
      '  f:readme.md',
      'd:src',
      '  f:main.go',
      'f:alpha.txt',
      'f:zebra.txt',
    ])
  })

  it('collapses a duplicate path onto the existing node', () => {
    const tree = buildTree(['a/b.go', 'a/b.go'])
    expect(tree).toHaveLength(1)
    expect(tree[0].children).toHaveLength(1)
  })
})
