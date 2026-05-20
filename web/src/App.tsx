import { Fragment, useEffect, useState } from 'react'
import { getBranches, getDiff, submitReview } from './api'
import { parseUnifiedDiff, type DiffLine, type FileDiff } from './diff'
import { highlightLine, langForPath } from './highlight'
import 'highlight.js/styles/github-dark.css'

type Mode = 'beads' | 'document' | 'direct'

interface AgentEvent {
  type: string
  text?: string
  tool?: string
}

interface Comment {
  id: string
  path: string
  line: number
  side: string
  body: string
  submitted: boolean
  collapsed: boolean
}

// Editor target: a line to comment on; `id` set means we're editing an existing one.
interface Editor {
  path: string
  line: number
  side: string
  id?: string
}

const rowId = (path: string, line: number) => `loc-${path.replace(/[^a-zA-Z0-9]/g, '_')}-${line}`

export default function App() {
  const [branches, setBranches] = useState<string[]>([])
  const [base, setBase] = useState('')
  const [branch, setBranch] = useState('')
  const [files, setFiles] = useState<FileDiff[]>([])
  const [comments, setComments] = useState<Comment[]>([])
  const [editor, setEditor] = useState<Editor | null>(null)
  const [draft, setDraft] = useState('')
  const [mode, setMode] = useState<Mode>('document')
  const [events, setEvents] = useState<AgentEvent[]>([])
  const [running, setRunning] = useState(false)
  const [flash, setFlash] = useState<string | null>(null)

  useEffect(() => {
    getBranches().then((info) => {
      setBranches(info.branches)
      setBase(info.base)
      if (info.current && info.current !== info.base) setBranch(info.current)
    })
  }, [])

  useEffect(() => {
    if (base && branch && base !== branch) {
      getDiff(base, branch).then((raw) => setFiles(parseUnifiedDiff(raw)))
    }
  }, [base, branch])

  const pending = comments.filter((c) => !c.submitted)

  function openNew(path: string, line: DiffLine) {
    const ln = line.newLine ?? line.oldLine ?? 0
    setEditor({ path, line: ln, side: line.kind === 'del' ? 'left' : 'right' })
    setDraft('')
  }

  function startEdit(c: Comment) {
    setEditor({ path: c.path, line: c.line, side: c.side, id: c.id })
    setDraft(c.body)
    jumpTo(c.path, c.line)
  }

  function saveEditor() {
    if (!editor) return
    const body = draft.trim()
    if (body) {
      if (editor.id) {
        // Editing reopens a comment so the next submission re-sends it.
        setComments((cs) =>
          cs.map((c) => (c.id === editor.id ? { ...c, body, submitted: false, collapsed: false } : c)),
        )
      } else {
        setComments((cs) => [
          ...cs,
          { id: crypto.randomUUID(), path: editor.path, line: editor.line, side: editor.side, body, submitted: false, collapsed: false },
        ])
      }
    }
    setEditor(null)
    setDraft('')
  }

  function deleteComment(id: string) {
    setComments((cs) => cs.filter((c) => c.id !== id))
    if (editor?.id === id) setEditor(null)
  }

  function toggleCollapse(id: string) {
    setComments((cs) => cs.map((c) => (c.id === id ? { ...c, collapsed: !c.collapsed } : c)))
  }

  function jumpTo(path: string, line: number) {
    const id = rowId(path, line)
    document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'center' })
    setFlash(id)
    setTimeout(() => setFlash((f) => (f === id ? null : f)), 1200)
  }

  async function submit() {
    if (running || !pending.length) return
    setRunning(true)
    setEvents([])
    const id = await submitReview({
      base,
      branch,
      mode,
      comments: pending.map(({ path, side, line, body }) => ({ path, side, line, body })),
    })
    // Mark everything we just sent as submitted + resolved so it won't resend.
    setComments((cs) => cs.map((c) => (c.submitted ? c : { ...c, submitted: true, collapsed: true })))
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${proto}://${location.host}/api/reviews/${id}/ws`)
    ws.onmessage = (e) => {
      const ev = JSON.parse(e.data) as AgentEvent
      setEvents((prev) => [...prev, ev])
      if (ev.type === 'result' || ev.type === 'error') setRunning(false)
    }
    ws.onclose = () => setRunning(false)
  }

  function editorRow() {
    return (
      <tr className="comment-edit">
        <td colSpan={3}>
          <textarea autoFocus value={draft} placeholder="Leave a comment…" onChange={(e) => setDraft(e.target.value)} />
          <div className="edit-actions">
            <button onClick={saveEditor}>{editor?.id ? 'Save' : 'Add'}</button>
            <button className="ghost" onClick={() => setEditor(null)}>Cancel</button>
          </div>
        </td>
      </tr>
    )
  }

  return (
    <div className="app">
      <header>
        <h1>loupe</h1>
        <div className="pickers">
          <label>
            base
            <select value={base} onChange={(e) => setBase(e.target.value)}>
              {branches.map((b) => <option key={b}>{b}</option>)}
            </select>
          </label>
          <span className="arrow">←</span>
          <label>
            branch
            <select value={branch} onChange={(e) => setBranch(e.target.value)}>
              <option value="">select…</option>
              {branches.map((b) => <option key={b}>{b}</option>)}
            </select>
          </label>
        </div>
      </header>

      <div className="layout">
        <main className="diff">
          {files.length === 0 && <p className="empty">Pick a branch to review.</p>}
          {files.map((f) => {
            const lang = langForPath(f.path)
            return (
              <section key={f.path} className="file">
                <div className="file-head">{f.path}</div>
                <table>
                  <tbody>
                    {f.hunks.map((h, hi) => (
                      <Fragment key={hi}>
                        <tr className="hunk-header">
                          <td colSpan={3}>{h.header}</td>
                        </tr>
                        {h.lines.map((l, li) => {
                          const ln = l.newLine ?? l.oldLine ?? 0
                          const id = rowId(f.path, ln)
                          const rowComments = comments.filter((c) => c.path === f.path && c.line === ln)
                          const editingHere = editor?.path === f.path && editor.line === ln
                          return (
                            <Fragment key={li}>
                              <tr id={id} className={`line ${l.kind} ${flash === id ? 'flash' : ''}`} onClick={() => openNew(f.path, l)}>
                                <td className="ln">{l.oldLine ?? ''}</td>
                                <td className="ln">{l.newLine ?? ''}</td>
                                <td className="content">
                                  <span className="sign">{sign(l.kind)}</span>
                                  <span className="code" dangerouslySetInnerHTML={{ __html: highlightLine(l.content, lang) }} />
                                </td>
                              </tr>
                              {rowComments.map((c) =>
                                editor?.id === c.id ? (
                                  <Fragment key={c.id}>{editorRow()}</Fragment>
                                ) : (
                                  <tr key={c.id} className={`comment-row ${c.submitted ? 'submitted' : ''}`}>
                                    <td colSpan={3}>
                                      {c.collapsed ? (
                                        <button className="comment-collapsed" onClick={() => toggleCollapse(c.id)}>
                                          ✓ resolved · {c.body.length > 60 ? c.body.slice(0, 60) + '…' : c.body}
                                          <span className="show">show</span>
                                        </button>
                                      ) : (
                                        <div className="comment-full">
                                          <div className="comment-body">
                                            💬 {c.body}
                                            {c.submitted && <span className="badge">submitted</span>}
                                          </div>
                                          <div className="comment-actions">
                                            <button onClick={() => startEdit(c)}>Edit</button>
                                            <button onClick={() => deleteComment(c.id)}>Delete</button>
                                            {c.submitted && <button onClick={() => toggleCollapse(c.id)}>Collapse</button>}
                                          </div>
                                        </div>
                                      )}
                                    </td>
                                  </tr>
                                ),
                              )}
                              {editingHere && !editor?.id && editorRow()}
                            </Fragment>
                          )
                        })}
                      </Fragment>
                    ))}
                  </tbody>
                </table>
              </section>
            )
          })}
        </main>

        <aside className="sidebar">
          <h2>Review · {pending.length} pending</h2>
          <ul className="comment-list">
            {comments.map((c) => (
              <li key={c.id} className={c.submitted ? 'submitted' : ''}>
                <div className="cl-main" onClick={() => jumpTo(c.path, c.line)} title="Jump to line">
                  <code>{c.path}:{c.line}</code>
                  <span className="cl-body">{c.body}</span>
                  {c.submitted && <span className="badge">✓</span>}
                </div>
                <div className="cl-actions">
                  <button onClick={() => startEdit(c)}>edit</button>
                  <button onClick={() => deleteComment(c.id)}>del</button>
                </div>
              </li>
            ))}
            {comments.length === 0 && <li className="muted">No comments yet — click a line to start.</li>}
          </ul>

          <label className="mode">
            Output
            <select value={mode} onChange={(e) => setMode(e.target.value as Mode)}>
              <option value="document">Document — plan</option>
              <option value="beads">Beads — epic + issues</option>
              <option value="direct">Direct — edit code</option>
            </select>
          </label>
          <button className="submit" disabled={!pending.length || running} onClick={submit}>
            {running ? 'Agent working…' : `Submit ${pending.length} comment${pending.length === 1 ? '' : 's'}`}
          </button>

          <h2>Agent</h2>
          <div className="console">
            {events.map((ev, i) => (
              <div key={i} className={`ev ev-${ev.type}`}>
                {ev.type === 'tool_use' ? `⚙ ${ev.tool}` : ev.text}
              </div>
            ))}
          </div>
        </aside>
      </div>
    </div>
  )
}

function sign(k: string) {
  return k === 'add' ? '+' : k === 'del' ? '-' : ' '
}
