import { useMemo, useState } from 'react'
import { buildTree, type TreeNode } from './filetree'

// Per-file decorations shown as badges in the tree. add/del are line counts from
// FileDiff.hunks; comments is the count of pending (unsubmitted) review comments.
export interface FileMeta {
  add: number
  del: number
  comments: number
}

interface FileTreeProps {
  paths: string[]
  activePath: string | null
  meta: Record<string, FileMeta>
  onSelect: (path: string) => void
}

// FileTree renders the changeset as a collapsible directory tree. Directory rows
// toggle their subtree; file rows call onSelect(path) to scroll the diff to that
// file. Collapsed-directory state is local — it is pure view state with no reason
// to live in App.
export function FileTree({ paths, activePath, meta, onSelect }: FileTreeProps) {
  const tree = useMemo(() => buildTree(paths), [paths])
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set())

  function toggle(path: string) {
    setCollapsed((prev) => {
      const next = new Set(prev)
      if (next.has(path)) next.delete(path)
      else next.add(path)
      return next
    })
  }

  function renderNodes(nodes: TreeNode[], depth: number) {
    return nodes.map((node) => {
      const indent = { paddingLeft: `${depth * 12 + 8}px` }
      if (node.type === 'dir') {
        const isCollapsed = collapsed.has(node.path)
        return (
          <div key={node.path} className="ft-group">
            <button type="button" className="ft-row ft-dir" style={indent} onClick={() => toggle(node.path)}>
              <span className="ft-caret">{isCollapsed ? '▸' : '▾'}</span>
              <span className="ft-name">{node.name}</span>
            </button>
            {!isCollapsed && renderNodes(node.children, depth + 1)}
          </div>
        )
      }
      const m = meta[node.path]
      const active = node.path === activePath
      return (
        <button
          type="button"
          key={node.path}
          className={`ft-row ft-file ${active ? 'active' : ''}`}
          style={indent}
          title={node.path}
          onClick={() => onSelect(node.path)}
        >
          <span className="ft-name">{node.name}</span>
          {m && (
            <span className="ft-badges">
              {m.comments > 0 && (
                <span className="ft-comments" title={`${m.comments} pending comment${m.comments === 1 ? '' : 's'}`}>
                  💬 {m.comments}
                </span>
              )}
              {m.add > 0 && <span className="ft-add">+{m.add}</span>}
              {m.del > 0 && <span className="ft-del">−{m.del}</span>}
            </span>
          )}
        </button>
      )
    })
  }

  if (paths.length === 0) {
    return <div className="ft-empty muted">No files in changeset.</div>
  }

  return <div className="filetree">{renderNodes(tree, 0)}</div>
}
