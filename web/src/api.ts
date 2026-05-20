export interface BranchInfo {
  branches: string[]
  base: string
  current: string
}

export interface CommentInput {
  path: string
  side: string
  line: number
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

export async function getBranches(): Promise<BranchInfo> {
  const r = await fetch('/api/branches')
  return r.json()
}

export async function getDiff(base: string, branch: string): Promise<string> {
  const r = await fetch(`/api/diff?base=${encodeURIComponent(base)}&branch=${encodeURIComponent(branch)}`)
  return (await r.json()).diff as string
}

// listReviews returns the stored reviews for a branch/base pair (newest first),
// each with its comments embedded, so the frontend can restore state on load.
export async function listReviews(base: string, branch: string): Promise<StoredReview[]> {
  const r = await fetch(`/api/reviews?base=${encodeURIComponent(base)}&branch=${encodeURIComponent(branch)}`)
  return ((await r.json()).reviews ?? []) as StoredReview[]
}

// ensureReview returns the id of the review for this branch/base, creating a
// draft one if needed. It does not start the agent.
export async function ensureReview(base: string, branch: string): Promise<number> {
  const r = await fetch('/api/reviews', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ base, branch }),
  })
  return (await r.json()).id as number
}

export async function addComment(reviewId: number, input: CommentInput): Promise<StoredComment> {
  const r = await fetch(`/api/reviews/${reviewId}/comments`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  })
  return (await r.json()) as StoredComment
}

export async function updateComment(
  id: number,
  patch: { body?: string; submitted?: boolean; collapsed?: boolean },
): Promise<void> {
  await fetch(`/api/comments/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch),
  })
}

export async function deleteComment(id: number): Promise<void> {
  await fetch(`/api/comments/${id}`, { method: 'DELETE' })
}

// submitReview marks the review's pending comments submitted and starts the agent.
export async function submitReview(reviewId: number, mode: string): Promise<void> {
  await fetch(`/api/reviews/${reviewId}/submit`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ mode }),
  })
}
