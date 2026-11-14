// Pure parts of handing a pull request's review comments to the agent
// (store/prAgent.ts): the settings, what there is to address, which pull
// request a command means and what to tell the user about a draft. Go builds
// the draft itself (internal/prfeedback): reviewers' text quoted, redacted
// and bounded.

/** The part of settings.json under "prReview" (Go: internal/settings/prreview.go). */
export interface PRReviewSettings {
  /** Unresolved threads one "Address review comments" draft carries. */
  maxThreads: number
  /** Lines of code around a commented line in the draft, on each side (0: none). */
  contextLines: number
}

/** Mirrors Go's defaultPRReview. */
export const DEFAULT_PR_REVIEW: PRReviewSettings = Object.freeze({ maxThreads: 20, contextLines: 8 })

/** Same bounds as Go (Min/MaxPRReview*). */
export const PR_REVIEW_LIMITS = {
  maxThreads: { min: 1, max: 50 },
  contextLines: { min: 0, max: 50 },
} as const

function clamp(key: keyof typeof PR_REVIEW_LIMITS, v: unknown): number {
  if (typeof v !== 'number' || !Number.isFinite(v) || (v === 0 && key === 'maxThreads')) return DEFAULT_PR_REVIEW[key]
  const { min, max } = PR_REVIEW_LIMITS[key]
  return Math.min(max, Math.max(min, Math.round(v)))
}

export function coercePRReview(v: unknown): PRReviewSettings {
  const o = typeof v === 'object' && v !== null ? (v as Record<string, unknown>) : {}
  return { maxThreads: clamp('maxThreads', o.maxThreads), contextLines: clamp('contextLines', o.contextLines) }
}

export interface ThreadLike {
  resolved: boolean
  outdated: boolean
  comments: readonly unknown[]
}

export interface ReviewLike {
  author: string
  state: string
  bodyHtml: string
}

/** "Ask agent" is offered on a thread that still waits for an answer. */
export const canAskAgent = (t: ThreadLike): boolean => !t.resolved && t.comments.length > 0

/** The threads "Address review comments" covers (Go: prfeedback.Unresolved). */
export const openThreads = (threads: readonly ThreadLike[]): number => threads.filter((t) => canAskAgent(t) && !t.outdated).length

/**
 * Reviews that request changes, say something and still stand: the reviewer's
 * later approval or a dismissal withdraws one (Go: prfeedback.ChangeRequests).
 */
export function changeRequests(reviews: readonly ReviewLike[]): number {
  const latest = new Map<string, number>()
  reviews.forEach((r, i) => {
    if (r.state === 'APPROVED' || r.state === 'CHANGES_REQUESTED' || r.state === 'DISMISSED') latest.set(r.author, i)
  })
  return reviews.filter((r, i) => r.state === 'CHANGES_REQUESTED' && latest.get(r.author) === i && r.bodyHtml.trim() !== '').length
}

export interface ReviewWork {
  threads: number
  reviews: number
}

/** What a loaded pull request has for the agent to address. */
export function reviewWork(d: { threads: readonly ThreadLike[]; reviews: readonly ReviewLike[] }): ReviewWork {
  return { threads: openThreads(d.threads), reviews: changeRequests(d.reviews) }
}

/** "3 unresolved threads · 1 review requests changes"; '' when there is nothing. */
export function workSummary(w: ReviewWork): string {
  const parts: string[] = []
  if (w.threads > 0) parts.push(`${w.threads} unresolved thread${w.threads === 1 ? '' : 's'}`)
  if (w.reviews > 0) parts.push(w.reviews === 1 ? '1 review requests changes' : `${w.reviews} reviews request changes`)
  return parts.join(' · ')
}

/**
 * What the "Your branch" banner says about review feedback, from data that is
 * already loaded: the pull request when its tab was opened, else the review
 * decision of its row in the list. null: nothing known to address.
 */
export function reviewHint(
  details: { threads: readonly ThreadLike[]; reviews: readonly ReviewLike[] } | null | undefined,
  decision: string | undefined,
): string | null {
  if (details) return workSummary(reviewWork(details)) || null
  return decision === 'CHANGES_REQUESTED' ? 'Changes requested' : null
}

export interface ReviewTarget {
  /** Folder of the repository, where the agent works. */
  root: string
  number: number
}

/**
 * The pull request the palette command means: the one in the active tab (its
 * tab or one of its diffs), else the open pull request of the current branch.
 */
export function pickReviewTarget(
  tab: { kind: string; root?: string; number?: number } | undefined,
  current: { root: string; pr: { number: number; state: string } | null | undefined } | null,
): ReviewTarget | null {
  if (tab && (tab.kind === 'pr' || tab.kind === 'prdiff') && tab.root && tab.number) return { root: tab.root, number: tab.number }
  if (current?.pr && current.pr.state === 'OPEN') return { root: current.root, number: current.pr.number }
  return null
}

/** Go's main.PRReviewDraft, as far as the notice reads it. */
export interface DraftLike {
  number: number
  threads: number
  leftOut: number
  headBranch: string
  branch: string
  onBranch: boolean
  open: boolean
}

/** A draft for the wrong branch is never sent on its own: the user checks out first. */
export const maySend = (d: DraftLike, send: boolean): boolean => send && d.onBranch

/** What to tell the user once the draft is in the chat (null: nothing). */
export function draftNotice(d: DraftLike): { message: string; kind: 'info' | 'error' } | null {
  if (!d.onBranch) {
    const here = d.branch ? `this folder is on ${d.branch}` : 'this folder has no branch checked out'
    const fix = d.open ? ' Check it out (pull request tab) before the agent changes files.' : ''
    return { message: `#${d.number}'s branch ${d.headBranch} isn't checked out: ${here}. The draft says so.${fix}`, kind: 'error' }
  }
  if (d.leftOut > 0) {
    return { message: `${d.threads} threads are in the draft; ${d.leftOut} more didn't fit and are listed at its end`, kind: 'info' }
  }
  return null
}

/** Whether an error from Go means there was nothing to hand over (prfeedback.ErrNothing). */
export const isNothingToAddress = (message: string): boolean => /no unresolved review comments/i.test(message)
