export interface BranchInfo {
  branches: string[]
  base: string
  current: string
}

export interface CommentInput {
  path: string
  side: string
  line: number
  // anchor_text is the content of the commented line, captured at creation so a
  // drifted comment can be relocated rather than misplaced. See anchor.ts.
  anchor_text: string
  body: string
}

// StoredComment mirrors store.Comment as serialized by the backend.
export interface StoredComment {
  id: number
  review_id: number
  path: string
  side: string
  line: number
  blob_sha: string
  anchor_text: string
  body: string
  submitted: boolean
  collapsed: boolean
}

export interface StoredReview {
  ID: number
  Branch: string
  Base: string
  Mode: string
  Status: string
  SessionID: string
  CreatedAt: string
  comments: StoredComment[] | null
}

// json parses a JSON response, rejecting on any non-2xx status with the
// response body as the error message. Every helper routes through this so a
// failed request surfaces instead of being silently treated as success.
async function json<T>(r: Response): Promise<T> {
  if (!r.ok) throw new Error(`${r.status} ${await r.text()}`)
  return r.json() as Promise<T>
}

// ok asserts a 2xx for endpoints with no useful body (e.g. 204 responses); it
// throws on failure like json() but returns nothing.
async function ok(r: Response): Promise<void> {
  if (!r.ok) throw new Error(`${r.status} ${await r.text()}`)
}

const JSON_HEADERS = { 'Content-Type': 'application/json' }

export async function getBranches(): Promise<BranchInfo> {
  return json<BranchInfo>(await fetch('/api/branches'))
}

export async function getDiff(base: string, branch: string): Promise<string> {
  const r = await fetch(`/api/diff?base=${encodeURIComponent(base)}&branch=${encodeURIComponent(branch)}`)
  return (await json<{ diff: string }>(r)).diff
}

// listReviews returns the stored reviews for a branch/base pair (newest first),
// each with its comments embedded, so the frontend can restore state on load.
export async function listReviews(base: string, branch: string): Promise<StoredReview[]> {
  const r = await fetch(`/api/reviews?base=${encodeURIComponent(base)}&branch=${encodeURIComponent(branch)}`)
  return (await json<{ reviews: StoredReview[] | null }>(r)).reviews ?? []
}

// ensureReview returns the id of the review for this branch/base, creating a
// draft one if needed. It does not start the agent.
export async function ensureReview(base: string, branch: string): Promise<number> {
  const r = await fetch('/api/reviews', {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify({ base, branch }),
  })
  return (await json<{ id: number }>(r)).id
}

export async function addComment(reviewId: number, input: CommentInput): Promise<StoredComment> {
  const r = await fetch(`/api/reviews/${reviewId}/comments`, {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify(input),
  })
  return json<StoredComment>(r)
}

export async function updateComment(
  id: number,
  patch: { body?: string; submitted?: boolean; collapsed?: boolean },
): Promise<void> {
  return ok(
    await fetch(`/api/comments/${id}`, {
      method: 'PATCH',
      headers: JSON_HEADERS,
      body: JSON.stringify(patch),
    }),
  )
}

export async function deleteComment(id: number): Promise<void> {
  return ok(await fetch(`/api/comments/${id}`, { method: 'DELETE' }))
}

// submitReview marks the review's pending comments submitted and starts the
// agent. It rejects (e.g. on a 400 "no pending comments") so callers don't open
// a WebSocket for a run that never started.
export async function submitReview(reviewId: number, mode: string): Promise<void> {
  return ok(
    await fetch(`/api/reviews/${reviewId}/submit`, {
      method: 'POST',
      headers: JSON_HEADERS,
      body: JSON.stringify({ mode }),
    }),
  )
}
