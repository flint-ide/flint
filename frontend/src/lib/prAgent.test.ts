import { it } from 'node:test'
import assert from 'node:assert/strict'
import {
  DEFAULT_PR_REVIEW,
  canAskAgent,
  changeRequests,
  coercePRReview,
  draftNotice,
  isNothingToAddress,
  maySend,
  openThreads,
  pickReviewTarget,
  reviewHint,
  reviewWork,
  workSummary,
  type DraftLike,
} from './prAgent'

const thread = (resolved = false, outdated = false, comments = 1) => ({ resolved, outdated, comments: Array(comments).fill({}) })
const review = (author: string, state: string, bodyHtml = '<p>x</p>') => ({ author, state, bodyHtml })

it('coercePRReview fills defaults and clamps like Go', () => {
  assert.deepEqual(coercePRReview(undefined), DEFAULT_PR_REVIEW)
  assert.deepEqual(coercePRReview({}), DEFAULT_PR_REVIEW)
  assert.deepEqual(coercePRReview({ maxThreads: 5, contextLines: 0 }), { maxThreads: 5, contextLines: 0 })
  assert.deepEqual(coercePRReview({ maxThreads: 0, contextLines: 3.4 }), { maxThreads: 20, contextLines: 3 })
  assert.deepEqual(coercePRReview({ maxThreads: 999, contextLines: -2 }), { maxThreads: 50, contextLines: 0 })
  assert.deepEqual(coercePRReview({ maxThreads: 'many', contextLines: NaN }), DEFAULT_PR_REVIEW)
})

it('threads the agent can be asked about', () => {
  assert.equal(canAskAgent(thread()), true)
  assert.equal(canAskAgent(thread(false, true)), true) // outdated: on its own, with a note
  assert.equal(canAskAgent(thread(true)), false)
  assert.equal(canAskAgent(thread(false, false, 0)), false)
  assert.equal(openThreads([thread(), thread(true), thread(false, true), thread(), thread(false, false, 0)]), 2)
})

it('change requests stand until approved or dismissed', () => {
  assert.equal(changeRequests([]), 0)
  assert.equal(changeRequests([review('dave', 'CHANGES_REQUESTED')]), 1)
  assert.equal(changeRequests([review('dave', 'CHANGES_REQUESTED'), review('dave', 'COMMENTED')]), 1)
  assert.equal(changeRequests([review('dave', 'CHANGES_REQUESTED'), review('dave', 'APPROVED')]), 0)
  assert.equal(changeRequests([review('dave', 'CHANGES_REQUESTED', '  '), review('carol', 'DISMISSED')]), 0)
  assert.equal(changeRequests([review('dave', 'CHANGES_REQUESTED', 'old'), review('dave', 'CHANGES_REQUESTED', 'new'), review('carol', 'CHANGES_REQUESTED')]), 2)
})

it('work summary and the banner hint', () => {
  const d = { threads: [thread(), thread()], reviews: [review('dave', 'CHANGES_REQUESTED')] }
  assert.deepEqual(reviewWork(d), { threads: 2, reviews: 1 })
  assert.equal(workSummary({ threads: 2, reviews: 1 }), '2 unresolved threads · 1 review requests changes')
  assert.equal(workSummary({ threads: 1, reviews: 0 }), '1 unresolved thread')
  assert.equal(workSummary({ threads: 0, reviews: 2 }), '2 reviews request changes')
  assert.equal(workSummary({ threads: 0, reviews: 0 }), '')
  assert.equal(reviewHint(d, undefined), '2 unresolved threads · 1 review requests changes')
  // Loaded details win over the list's decision: every thread may be resolved by now.
  assert.equal(reviewHint({ threads: [thread(true)], reviews: [] }, 'CHANGES_REQUESTED'), null)
  assert.equal(reviewHint(null, 'CHANGES_REQUESTED'), 'Changes requested')
  assert.equal(reviewHint(undefined, 'APPROVED'), null)
  assert.equal(reviewHint(undefined, undefined), null)
})

it('the command picks the active pull request, else the current branch', () => {
  const current = { root: '/repo', pr: { number: 7, state: 'OPEN' } }
  assert.deepEqual(pickReviewTarget({ kind: 'pr', root: '/p', number: 3 }, current), { root: '/p', number: 3 })
  assert.deepEqual(pickReviewTarget({ kind: 'prdiff', root: '/p', number: 4 }, null), { root: '/p', number: 4 })
  assert.deepEqual(pickReviewTarget({ kind: 'file' }, current), { root: '/repo', number: 7 })
  assert.deepEqual(pickReviewTarget(undefined, current), { root: '/repo', number: 7 })
  assert.equal(pickReviewTarget(undefined, { root: '/repo', pr: { number: 7, state: 'MERGED' } }), null)
  assert.equal(pickReviewTarget({ kind: 'file' }, { root: '/repo', pr: null }), null)
  assert.equal(pickReviewTarget(undefined, null), null)
})

const draft = (over: Partial<DraftLike> = {}): DraftLike => ({
  number: 7, threads: 2, leftOut: 0, headBranch: 'feat/x', branch: 'feat/x', onBranch: true, open: true, ...over,
})

it('a draft for another branch is never sent on its own', () => {
  assert.equal(maySend(draft(), true), true)
  assert.equal(maySend(draft(), false), false)
  assert.equal(maySend(draft({ onBranch: false, branch: 'main' }), true), false)
})

it('draft notices', () => {
  assert.equal(draftNotice(draft()), null)
  assert.deepEqual(draftNotice(draft({ onBranch: false, branch: 'main' })), {
    message: "#7's branch feat/x isn't checked out: this folder is on main. The draft says so. Check it out (pull request tab) before the agent changes files.",
    kind: 'error',
  })
  assert.match(draftNotice(draft({ onBranch: false, branch: '', open: false }))!.message, /this folder has no branch checked out\. The draft says so\.$/)
  assert.deepEqual(draftNotice(draft({ threads: 20, leftOut: 5 })), { message: "20 threads are in the draft; 5 more didn't fit and are listed at its end", kind: 'info' })
})

it('nothing to address is not an error', () => {
  assert.equal(isNothingToAddress('no unresolved review comments on #7'), true)
  assert.equal(isNothingToAddress('pull request #7 not found in acme/widgets'), false)
})
