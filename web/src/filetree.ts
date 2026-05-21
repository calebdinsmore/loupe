// Folds a flat list of changeset paths into a nested directory/file tree for the
// navigation sidebar. Pure + unit-tested (filetree.test.ts); the only piece of
// the sidebar with real logic.

export interface TreeNode {
  // Segment name: the directory name, or the file's basename.
  name: string
  // Full slash-joined path from the root. For files this is the original path;
  // for directories it is the prefix up to and including this segment.
  path: string
  type: 'dir' | 'file'
  // Always present; empty for files.
  children: TreeNode[]
}

// buildTree folds paths like "a/b/c.go" into nested dir/file nodes. Siblings are
// ordered directories-first, then alphabetically by name. Empty/blank segments
// (leading or doubled slashes) are ignored, and a duplicate path collapses onto
// the existing node rather than creating a second one.
export function buildTree(paths: string[]): TreeNode[] {
  const root: TreeNode[] = []

  for (const raw of paths) {
    const segments = raw.split('/').filter(Boolean)
    if (segments.length === 0) continue

    let level = root
    let prefix = ''
    for (let i = 0; i < segments.length; i++) {
      const seg = segments[i]
      prefix = prefix ? `${prefix}/${seg}` : seg
      const isFile = i === segments.length - 1
      const type = isFile ? 'file' : 'dir'
      let node = level.find((n) => n.name === seg && n.type === type)
      if (!node) {
        node = { name: seg, path: prefix, type, children: [] }
        level.push(node)
      }
      if (!isFile) level = node.children
    }
  }

  sortTree(root)
  return root
}

function sortTree(nodes: TreeNode[]): void {
  nodes.sort((a, b) => {
    if (a.type !== b.type) return a.type === 'dir' ? -1 : 1
    return a.name.localeCompare(b.name)
  })
  for (const n of nodes) {
    if (n.type === 'dir') sortTree(n.children)
  }
}
