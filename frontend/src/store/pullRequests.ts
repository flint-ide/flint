// Pull request review through gh (Go: internal/github). Nothing here polls: the list
// loads when the view opens or the window regains focus, a pull request when its tab
// opens or regains focus, and both on Refresh. Go caches answers for ~30 s.
import { createStore } from '../lib/store'
import {
  GitHub,
  errorMessage,
  newReviewComment,
  pullRequestFilter,
  type GitHubStatus,
  type PullRequest,
  type PullRequestDetails,
} from '../api/backend'
import { BrowserOpenURL } from '../../wailsjs/runtime/runtime'
import { confirmAction, showToast } from './dialog'
import { useEditor } from './editor'

export type Who = 'all' | 'mine' | 'review'
export type ListState = 'open' | 'closed' | 'merged' | 'all'
export type ReviewEvent = 'COMMENT' | 'APPROVE' | 'REQUEST_CHANGES'

export interface DetailsEntry {
  data: PullRequestDetails | null
  loading: boolean
  error: string | null
}

interface PullRequestsState {
  /** Project folder whose repository the list shows. */
  root: string | null
  who: Who
  state: ListState
  status: GitHubStatus | null
  items: PullRequest[]
  loading: boolean
  error: string | null
  /** Pull requests opened in tabs, by detailsKey. */
  details: Record<string, DetailsEntry>
  /** What a pull request is busy with (e.g. "submit"), by detailsKey. */
  busy: Record<string, string>
}

export const detailsKey = (root: string, number: number) => `${root}#${number}`

export const usePullRequests = createStore<PullRequestsState>(() => ({
  root: null,
  who: 'all',
  state: 'open',
  status: null,
  items: [],
  loading: false,
  error: null,
  details: {},
  busy: {},
}))

const set = usePullRequests.setState
const get = usePullRequests.getState

let listSeq = 0

/** Loads the list for root (checking gh first); force skips Go's cache. */
export async function loadPullRequests(root: string | null, force = false): Promise<void> {
  const seq = ++listSeq
  const switched = root !== get().root
  set({ root, loading: !!root, error: null, ...(switched ? { items: [], status: null } : {}) })
  if (!root) return
  try {
    const status = await GitHub.Check(root, force)
    if (seq !== listSeq) return
    if (status.state !== 'ok') {
      set({ status, items: [], loading: false })
      return
    }
    const { who, state } = get()
    const items = await GitHub.List(root, pullRequestFilter({ who, state }), force)
    if (seq !== listSeq) return
    set({ status, items: items ?? [], loading: false })
  } catch (err) {
    if (seq === listSeq) set({ error: errorMessage(err), loading: false })
  }
}

export function setListFilter(filter: { who?: Who; state?: ListState }): void {
  set(filter)
  void loadPullRequests(get().root)
}

const inflight = new Map<string, Promise<void>>()

/** Loads (or refreshes) a pull request shown in a tab; concurrent calls share one request. */
export function loadDetails(root: string, number: number, force = false): Promise<void> {
  const key = detailsKey(root, number)
  const running = inflight.get(key)
  if (running && !force) return running
  const empty: DetailsEntry = { data: null, loading: false, error: null }
  const patch = (entry: Partial<DetailsEntry>) =>
    set((s) => ({ details: { ...s.details, [key]: { ...empty, ...s.details[key], ...entry } } }))
  patch({ loading: true })
  const job = GitHub.Details(root, number, force)
    .then((data) => patch({ data, loading: false, error: null }))
    .catch((err) => patch({ loading: false, error: errorMessage(err) }))
    .finally(() => {
      if (inflight.get(key) === job) inflight.delete(key)
    })
  inflight.set(key, job)
  return job
}

/** Forgets a closed pull request tab's data. */
export function dropDetails(root: string, number: number): void {
  const key = detailsKey(root, number)
  const stillOpen = useEditor.getState().tabs.some((t) => (t.kind === 'pr' || t.kind === 'prdiff') && t.root === root && t.number === number)
  if (stillOpen || !(key in get().details)) return
  set((s) => {
    const { [key]: _gone, ...details } = s.details
    return { details }
  })
}

export function openPullRequest(root: string, pr: { number: number; title: string }): void {
  useEditor.getState().openPullRequest(root, pr.number, pr.title)
  void loadDetails(root, pr.number)
}

export function openPullRequestFile(root: string, number: number, path: string): void {
  useEditor.getState().openPullRequestDiff(root, number, path)
}

export function openInBrowser(url: string): void {
  if (/^https?:\/\//i.test(url)) BrowserOpenURL(url)
}

/**
 * Runs a review action: marks the pull request busy, reports errors as a toast and
 * reloads the pull request afterwards. Returns whether the action succeeded.
 */
async function act(root: string, number: number, what: string, run: () => Promise<void>): Promise<boolean> {
  const key = detailsKey(root, number)
  set((s) => ({ busy: { ...s.busy, [key]: what } }))
  try {
    await run()
    await loadDetails(root, number, true)
    return true
  } catch (err) {
    showToast(errorMessage(err), 'error')
    return false
  } finally {
    set((s) => {
      const { [key]: _done, ...busy } = s.busy
      return { busy }
    })
  }
}

export function replyToThread(root: string, number: number, threadId: string, body: string): Promise<boolean> {
  return act(root, number, 'reply', () => GitHub.Reply(root, number, threadId, body))
}

export function setThreadResolved(root: string, number: number, threadId: string, resolved: boolean): Promise<boolean> {
  return act(root, number, resolved ? 'resolve' : 'unresolve', () => GitHub.SetResolved(root, number, threadId, resolved))
}

export function addReviewComment(
  root: string,
  number: number,
  c: { path: string; line: number; side: 'LEFT' | 'RIGHT'; body: string; review: boolean },
): Promise<boolean> {
  return act(root, number, 'comment', () => GitHub.AddComment(root, number, newReviewComment({ ...c, startLine: 0 })))
}

export async function submitReview(root: string, number: number, event: ReviewEvent, body: string): Promise<boolean> {
  const ok = await act(root, number, 'submit', () => GitHub.SubmitReview(root, number, event, body))
  if (ok) {
    showToast(event === 'APPROVE' ? `Approved #${number}` : event === 'REQUEST_CHANGES' ? `Requested changes on #${number}` : `Review submitted on #${number}`)
    if (get().root === root) void loadPullRequests(root, true) // the review decision changed
  }
  return ok
}

export async function discardReview(root: string, number: number, comments: number): Promise<void> {
  const what = comments === 1 ? 'its comment' : `its ${comments} comments`
  if (!(await confirmAction(`Discard your pending review and ${what}?`, 'Discard'))) return
  await act(root, number, 'discard', () => GitHub.DiscardReview(root, number))
}

export async function checkoutPullRequest(root: string, number: number, branch: string): Promise<void> {
  if (!(await confirmAction(`Check out #${number} (${branch}) in this project?`, 'Check Out'))) return
  const ok = await act(root, number, 'checkout', () => GitHub.Checkout(root, number))
  if (ok) showToast(`Checked out #${number} (${branch})`)
}
