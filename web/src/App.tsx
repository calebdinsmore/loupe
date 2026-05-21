import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import { FileTree, type FileMeta } from './FileTreePanel'
import {
  addComment,
  deleteComment as apiDeleteComment,
  ensureReview,
  getBranches,
  getDiff,
  listPrompts,
  listReviews,
  type PromptInfo,
  submitReview,
  updateComment,
} from './api'
import { relocate } from './anchor'
import { parseUnifiedDiff, type DiffLine, type FileDiff } from './diff'
import { appendEvent, type AgentEvent } from './events'
import { highlightLine, langForPath } from './highlight'
import { formatClock, minuteBucket } from './timestamps'
import 'highlight.js/styles/github-dark.css'

// WORKING_REF mirrors git.WorkingRef: a sentinel "branch" that selects the
// working tree (committed + uncommitted changes) instead of a real branch. It
// flows through the existing base/branch UI, persistence key, and WebSocket
// untouched, so no API shape changes are needed.
const WORKING_REF = '*working*'

interface Comment {
  id: number
  path: string
  line: number
  side: string
  // anchorText is the commented line's content at creation, used to relocate the
  // comment when the diff drifts. Empty for legacy comments (treated as no anchor).
  anchorText: string
  body: string
  submitted: boolean
  collapsed: boolean
}

// Editor target: a line to comment on; `id` set means we're editing an existing one.
interface Editor {
  path: string
  line: number
  side: string
  anchorText: string
  id?: number
}

const rowId = (path: string, line: number) => `loc-${path.replace(/[^a-zA-Z0-9]/g, '_')}-${line}`
// Stable scroll-target id for a file's <section>, used by the tree sidebar.
const fileId = (path: string) => `file-${path.replace(/[^a-zA-Z0-9]/g, '_')}`

export default function App() {
  const [branches, setBranches] = useState<string[]>([])
  const [base, setBase] = useState('')
  const [branch, setBranch] = useState('')
  const [files, setFiles] = useState<FileDiff[]>([])
  const [comments, setComments] = useState<Comment[]>([])
  const [reviewId, setReviewId] = useState<number | null>(null)
  const [editor, setEditor] = useState<Editor | null>(null)
  const [draft, setDraft] = useState('')
  const [mode, setMode] = useState<string>('document')
  const [prompts, setPrompts] = useState<PromptInfo[]>([])
  const [events, setEvents] = useState<AgentEvent[]>([])
  const consoleRef = useRef<HTMLDivElement>(null)
  const [running, setRunning] = useState(false)
  const [flash, setFlash] = useState<string | null>(null)
  // Bumped by the Refresh button to re-pull the diff. Working-tree diffs are
  // live, so unlike committed branch diffs they can change without base/branch
  // changing.
  const [refreshKey, setRefreshKey] = useState(0)
  // Left file-tree nav: whether the panel is collapsed to a rail, and which file
  // is currently in view (scroll-spy highlight).
  const [treeCollapsed, setTreeCollapsed] = useState(false)
  const [activeFile, setActiveFile] = useState<string | null>(null)

  // Follow new agent output, but only when the user is already near the bottom.
  // The 80px threshold tolerates the height of the row just appended, so a user
  // who has scrolled up to read history is not yanked back down.
  useEffect(() => {
    const el = consoleRef.current
    if (!el) return
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80
    if (nearBottom) el.scrollTop = el.scrollHeight
  }, [events])

  useEffect(() => {
    getBranches().then((info) => {
      setBranches(info.branches)
      setBase(info.base)
      if (info.current && info.current !== info.base) setBranch(info.current)
    })
  }, [])

  // Hydrate the Output dropdown from the server: the available prompts and the
  // last-submitted selection (server-driven, no localStorage).
  useEffect(() => {
    listPrompts().then(({ prompts, selected }) => {
      setPrompts(prompts)
      setMode(selected)
    })
  }, [])

  // Pull the diff. Separate from review hydration so the Refresh button can
  // re-pull a live working-tree diff without resetting comment state.
  useEffect(() => {
    if (!(base && branch && base !== branch)) return
    let cancelled = false
    getDiff(base, branch).then((raw) => {
      if (!cancelled) setFiles(parseUnifiedDiff(raw))
    })
    return () => {
      cancelled = true
    }
  }, [base, branch, refreshKey])

  useEffect(() => {
    if (!(base && branch && base !== branch)) return
    let cancelled = false
    // Reattach to the persisted review for this pair so submitted comments
    // reappear (collapsed/resolved) after a reload.
    setReviewId(null)
    setComments([])
    ;(async () => {
      const id = await ensureReview(base, branch)
      if (cancelled) return
      setReviewId(id)
      const reviews = await listReviews(base, branch)
      if (cancelled) return
      const rev = reviews.find((r) => r.ID === id)
      const hydrated = (rev?.comments ?? []).map((c) => ({
        id: c.id,
        path: c.path,
        line: c.line,
        side: c.side,
        anchorText: c.anchor_text,
        body: c.body,
        submitted: c.submitted,
        collapsed: c.collapsed,
      }))
      setComments(hydrated)
    })()
    return () => {
      cancelled = true
    }
  }, [base, branch])

  const pending = comments.filter((c) => !c.submitted)

  const paths = useMemo(() => files.map((f) => f.path), [files])

  // Console rows with timestamp dividers. Walk the events once, tracking the
  // previous rendered event's minute bucket; flag showTs on the first event with
  // a ts and whenever output crosses into a new minute. Built as an explicit
  // loop to avoid closure-over-loop pitfalls in the render.
  const consoleRows = useMemo(() => {
    const rows: { event: AgentEvent; showTs: boolean }[] = []
    let prevBucket: number | null = null
    for (const event of events) {
      let showTs = false
      if (event.ts != null) {
        const bucket = minuteBucket(event.ts)
        if (bucket !== prevBucket) {
          showTs = true
          prevBucket = bucket
        }
      }
      rows.push({ event, showTs })
    }
    return rows
  }, [events])

  // Per-file badge data for the tree: +/- line counts from the diff plus the
  // count of pending comments anchored in each file.
  const fileMeta = useMemo(() => {
    const m: Record<string, FileMeta> = {}
    for (const f of files) {
      let add = 0
      let del = 0
      for (const h of f.hunks) {
        for (const l of h.lines) {
          if (l.kind === 'add') add++
          else if (l.kind === 'del') del++
        }
      }
      m[f.path] = { add, del, comments: 0 }
    }
    for (const c of comments) {
      if (!c.submitted && m[c.path]) m[c.path].comments++
    }
    return m
  }, [files, comments])

  // Resolve every comment against the freshly parsed diff. A comment whose stored
  // line still matches (or relocates within the search window) renders inline at
  // its current line; one that can't be placed is collected as orphaned so it can
  // surface in the per-file "outdated comments" section instead of vanishing.
  const located = useMemo(() => {
    const byPath = new Map<string, { inline: Map<number, Comment[]>; orphaned: Comment[] }>()
    for (const f of files) {
      const entry = { inline: new Map<number, Comment[]>(), orphaned: [] as Comment[] }
      for (const c of comments) {
        if (c.path !== f.path) continue
        const ln = relocate({ line: c.line, anchorText: c.anchorText }, f)
        if (ln == null) {
          entry.orphaned.push(c)
        } else {
          const arr = entry.inline.get(ln) ?? []
          arr.push(c)
          entry.inline.set(ln, arr)
        }
      }
      byPath.set(f.path, entry)
    }
    return byPath
  }, [files, comments])

  // Scroll-spy: highlight the file whose section is most prominently in view.
  useEffect(() => {
    const root = document.querySelector('.diff')
    if (!root || files.length === 0) return
    const sections = Array.from(root.querySelectorAll<HTMLElement>('section.file[data-path]'))
    if (sections.length === 0) return
    const ratios = new Map<string, number>()
    const obs = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          const p = (e.target as HTMLElement).dataset.path
          if (p) ratios.set(p, e.isIntersecting ? e.intersectionRatio : 0)
        }
        let best: string | null = null
        let bestRatio = 0
        for (const [p, r] of ratios) {
          if (r > bestRatio) {
            bestRatio = r
            best = p
          }
        }
        // Always assign (including null) so the highlight clears when every
        // section has scrolled out of view rather than going stale.
        setActiveFile(best)
      },
      { root, threshold: [0, 0.25, 0.5, 1] },
    )
    sections.forEach((s) => obs.observe(s))
    return () => obs.disconnect()
  }, [files])

  function openNew(path: string, line: DiffLine) {
    const ln = line.newLine ?? line.oldLine ?? 0
    // Capture the line's content as the anchor so the comment can relocate if the
    // diff later shifts.
    setEditor({ path, line: ln, side: line.kind === 'del' ? 'left' : 'right', anchorText: line.content })
    setDraft('')
  }

  function startEdit(c: Comment) {
    setEditor({ path: c.path, line: c.line, side: c.side, anchorText: c.anchorText, id: c.id })
    setDraft(c.body)
    jumpTo(c.path, c.line)
  }

  // ensureReviewId returns the persisted review id for the current pair, creating
  // a draft one the first time a comment is added.
  async function ensureReviewId(): Promise<number> {
    if (reviewId != null) return reviewId
    const id = await ensureReview(base, branch)
    setReviewId(id)
    return id
  }

  async function saveEditor() {
    if (!editor) return
    const body = draft.trim()
    const target = editor
    setEditor(null)
    setDraft('')
    if (!body) return
    if (target.id != null) {
      // Editing reopens a comment so the next submission re-sends it.
      await updateComment(target.id, { body, submitted: false, collapsed: false })
      setComments((cs) =>
        cs.map((c) => (c.id === target.id ? { ...c, body, submitted: false, collapsed: false } : c)),
      )
    } else {
      const id = await ensureReviewId()
      const created = await addComment(id, {
        path: target.path,
        side: target.side,
        line: target.line,
        anchor_text: target.anchorText,
        body,
      })
      setComments((cs) => [
        ...cs,
        { id: created.id, path: created.path, line: created.line, side: created.side, anchorText: created.anchor_text, body: created.body, submitted: created.submitted, collapsed: created.collapsed },
      ])
    }
  }

  async function deleteComment(id: number) {
    if (editor?.id === id) setEditor(null)
    await apiDeleteComment(id)
    setComments((cs) => cs.filter((c) => c.id !== id))
  }

  async function toggleCollapse(id: number) {
    const current = comments.find((c) => c.id === id)
    if (!current) return
    const collapsed = !current.collapsed
    await updateComment(id, { collapsed })
    setComments((cs) => cs.map((c) => (c.id === id ? { ...c, collapsed } : c)))
  }

  function jumpTo(path: string, line: number) {
    const id = rowId(path, line)
    document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'center' })
    setFlash(id)
    setTimeout(() => setFlash((f) => (f === id ? null : f)), 1200)
  }

  // Scroll the diff to a file's section header, reusing jumpTo's flash pattern.
  function scrollToFile(path: string) {
    const id = fileId(path)
    document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
    setFlash(id)
    setTimeout(() => setFlash((f) => (f === id ? null : f)), 1200)
  }

  async function submit() {
    if (running || !pending.length) return
    setRunning(true)
    setEvents([])
    let id: number
    try {
      // Resolve the review id and mark the batch submitted before opening the
      // socket. If either rejects (e.g. a 400 "no pending comments"), surface it
      // and bail — we must not open a WebSocket for a run that never started.
      id = await ensureReviewId()
      await submitReview(id, mode)
    } catch (err) {
      setRunning(false)
      setEvents([{ type: 'error', text: err instanceof Error ? err.message : String(err) }])
      return
    }
    // The server marked the pending batch submitted + collapsed; mirror that
    // locally so the UI resolves them and the submit button gates correctly.
    setComments((cs) => cs.map((c) => (c.submitted ? c : { ...c, submitted: true, collapsed: true })))
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${proto}://${location.host}/api/reviews/${id}/ws`)
    ws.onmessage = (e) => {
      const ev = JSON.parse(e.data) as AgentEvent
      if (ev.type === 'result' || ev.type === 'error') setRunning(false)
      setEvents((prev) => appendEvent(prev, ev))
    }
    ws.onclose = () => setRunning(false)
  }

  // editorForm is the textarea + actions, reused both inline in the diff table
  // (wrapped in a row) and standalone in the outdated-comments section.
  function editorForm() {
    return (
      <>
        <textarea autoFocus value={draft} placeholder="Leave a comment…" onChange={(e) => setDraft(e.target.value)} />
        <div className="edit-actions">
          <button onClick={saveEditor}>{editor?.id ? 'Save' : 'Add'}</button>
          <button className="ghost" onClick={() => setEditor(null)}>Cancel</button>
        </div>
      </>
    )
  }

  function editorRow() {
    return (
      <tr className="comment-edit">
        <td colSpan={3}>{editorForm()}</td>
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
              <option value={WORKING_REF}>Working tree (uncommitted)</option>
              {branches.map((b) => <option key={b}>{b}</option>)}
            </select>
          </label>
          <button
            className="refresh"
            onClick={() => setRefreshKey((k) => k + 1)}
            disabled={!(base && branch && base !== branch)}
            title="Re-pull the diff (working-tree diffs are live)"
          >
            Refresh diff
          </button>
        </div>
      </header>

      <div className={`layout ${treeCollapsed ? 'tree-collapsed' : ''}`}>
        {treeCollapsed ? (
          <div className="filetree-rail">
            <button className="ft-toggle" onClick={() => setTreeCollapsed(false)} title="Show file tree">
              ›
            </button>
          </div>
        ) : (
          <aside className="filetree-panel">
            <div className="ft-head">
              <span>Files</span>
              <button className="ft-toggle" onClick={() => setTreeCollapsed(true)} title="Collapse file tree">
                ‹
              </button>
            </div>
            <FileTree paths={paths} activePath={activeFile} meta={fileMeta} onSelect={scrollToFile} />
          </aside>
        )}
        <main className="diff">
          {files.length === 0 && <p className="empty">Pick a branch to review.</p>}
          {files.map((f) => {
            const lang = langForPath(f.path)
            const loc = located.get(f.path)
            const orphaned = loc?.orphaned ?? []
            return (
              <section key={f.path} id={fileId(f.path)} data-path={f.path} className={`file ${flash === fileId(f.path) ? 'flash' : ''}`}>
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
                          const rowComments = loc?.inline.get(ln) ?? []
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
                {orphaned.length > 0 && (
                  <details className="outdated">
                    <summary>
                      {orphaned.length} outdated comment{orphaned.length === 1 ? '' : 's'} · no longer match a line
                    </summary>
                    <ul className="outdated-list">
                      {orphaned.map((c) => (
                        <li key={c.id}>
                          {editor?.id === c.id ? (
                            editorForm()
                          ) : (
                            <>
                              <div className="comment-body">
                                💬 {c.body}
                                <span className="badge">was line {c.line}</span>
                              </div>
                              <div className="comment-actions">
                                <button onClick={() => startEdit(c)}>Edit</button>
                                <button onClick={() => deleteComment(c.id)}>Delete</button>
                              </div>
                            </>
                          )}
                        </li>
                      ))}
                    </ul>
                  </details>
                )}
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
            <select value={mode} onChange={(e) => setMode(e.target.value)}>
              {prompts.map((p) => (
                <option key={p.ID} value={p.ID}>
                  {p.Name}
                </option>
              ))}
            </select>
          </label>
          <button className="submit" disabled={!pending.length || running} onClick={submit}>
            {running ? 'Agent working…' : `Submit ${pending.length} comment${pending.length === 1 ? '' : 's'}`}
          </button>

          <h2>Agent</h2>
          <div className="console" ref={consoleRef}>
            {consoleRows.map(({ event: ev, showTs }, i) => (
              <Fragment key={ev.ts != null ? `${ev.ts}-${i}` : i}>
                {showTs && ev.ts != null && (
                  <div className="ev-ts">{formatClock(ev.ts)}</div>
                )}
                <div className={`ev ev-${ev.type}`}>
                  {ev.type === 'tool_use' ? `⚙ ${ev.tool}` : ev.text}
                </div>
              </Fragment>
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
