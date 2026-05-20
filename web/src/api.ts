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

export async function getBranches(): Promise<BranchInfo> {
  const r = await fetch('/api/branches')
  return r.json()
}

export async function getDiff(base: string, branch: string): Promise<string> {
  const r = await fetch(`/api/diff?base=${encodeURIComponent(base)}&branch=${encodeURIComponent(branch)}`)
  return (await r.json()).diff as string
}

export async function submitReview(payload: {
  branch: string
  base: string
  mode: string
  comments: CommentInput[]
}): Promise<number> {
  const r = await fetch('/api/reviews', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  return (await r.json()).id as number
}
