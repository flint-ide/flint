import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  ciNotice,
  ciState,
  ciFixPrompt,
  ciTransition,
  coerceCI,
  DEFAULT_CI,
  failedRuns,
  fence,
  nextPollMs,
  type CIReportLike,
} from './ci'

const counts = (c: Partial<Record<'success' | 'failure' | 'pending' | 'skipped' | 'neutral', number>>) => ({
  success: 0, failure: 0, pending: 0, skipped: 0, neutral: 0, ...c,
})

describe('ciState', () => {
  it('is none without a pull request or checks', () => {
    assert.equal(ciState(null), 'none')
    assert.equal(ciState({ pr: null, counts: counts({}) }), 'none')
    assert.equal(ciState({ pr: { number: 1 }, counts: counts({}) }), 'none')
  })
  it('fails on any failure, else pends, else passes', () => {
    const pr = { number: 1 }
    assert.equal(ciState({ pr, counts: counts({ failure: 1, pending: 2 }) }), 'failure')
    assert.equal(ciState({ pr, counts: counts({ pending: 1, success: 3 }) }), 'pending')
    assert.equal(ciState({ pr, counts: counts({ success: 3, skipped: 1 }) }), 'success')
  })
})

describe('ciTransition', () => {
  it('tells only about changes seen while following', () => {
    assert.equal(ciTransition(undefined, 'failure'), null)
    assert.equal(ciTransition('pending', 'failure'), 'failed')
    assert.equal(ciTransition('success', 'failure'), 'failed')
    assert.equal(ciTransition('failure', 'failure'), null)
    assert.equal(ciTransition('pending', 'success'), 'passed')
    assert.equal(ciTransition('failure', 'success'), 'passed')
    assert.equal(ciTransition('success', 'success'), null)
    assert.equal(ciTransition('failure', 'pending'), null)
    assert.equal(ciTransition('none', 'success'), null)
  })
})

describe('nextPollMs', () => {
  it('backs off from the interval and caps it', () => {
    assert.equal(nextPollMs(30, 0, true), 30_000)
    assert.equal(nextPollMs(30, 1, true), 45_000)
    assert.equal(nextPollMs(30, 50, true), 240_000) // 8× the interval
    assert.equal(nextPollMs(600, 50, true), 1_800_000) // never more than 30 min
  })
  it('polls half as often in the background', () => {
    assert.equal(nextPollMs(30, 0, false), 60_000)
  })
})

describe('failedRuns', () => {
  it('groups failed Actions checks by run', () => {
    const runs = failedRuns([
      { name: 'test', workflow: 'CI', state: 'failure', runId: 5, jobId: 51 },
      { name: 'lint', workflow: 'CI', state: 'failure', runId: 5, jobId: 52 },
      { name: 'docs', workflow: 'Docs', state: 'success', runId: 6, jobId: 61 },
      { name: 'ext', workflow: '', state: 'failure', runId: 0, jobId: 0 },
      { name: 'e2e', workflow: 'E2E', state: 'failure', runId: 7, jobId: 0 },
    ])
    assert.deepEqual(runs, [
      { runId: 5, jobIds: [51, 52], names: ['CI / test', 'CI / lint'] },
      { runId: 7, jobIds: [], names: ['E2E / e2e'] },
    ])
  })
})

const report: CIReportLike = {
  runId: 41307693761,
  workflow: 'CI',
  title: 'feat: rate limiting',
  url: 'https://github.com/acme/flint/actions/runs/41307693761',
  branch: 'feat/rate-limit',
  sha: '785d8f7a66d3403ed32421b45c87ede4fbda8bda',
  attempt: 2,
  jobs: [
    {
      name: 'Build and test (macOS)',
      url: 'https://github.com/acme/flint/actions/runs/41307693761/job/124851367741',
      step: 'go test',
      stepNumber: 12,
      lines: ['…', '--- FAIL: TestStop (4.62s)', '    review_test.go:316: Stop took 4.3s', 'FAIL'],
      omitted: 40,
      noLog: false,
      files: [
        { text: 'review_test.go:316', path: 'internal/remote/review_test.go', line: 316, column: 0 },
        { text: 'internal/ratelimit/bucket.go:42:9', path: 'internal/ratelimit/bucket.go', line: 42, column: 9 },
      ],
    },
    { name: 'Lint', url: '', step: 'golangci', stepNumber: 3, lines: [], omitted: 0, noLog: true, files: [] },
  ],
}

describe('ciFixPrompt', () => {
  const pr = { number: 29, title: 'feat: rate limiting', url: 'https://github.com/acme/flint/pull/29' }
  it('says what failed, where, with the excerpt and the files', () => {
    const p = ciFixPrompt([report], { pr, workspaceBranch: 'feat/rate-limit' })
    assert.match(p, /CI failed on pull request #29 "feat: rate limiting" \(https:\/\/github\.com\/acme\/flint\/pull\/29\)/)
    assert.match(p, /branch feat\/rate-limit at commit 785d8f7a66d3/)
    assert.match(p, /Workflow "CI" \(attempt 2\): https:\/\/github\.com\/acme\/flint\/actions\/runs\/41307693761/)
    assert.match(p, /Job "Build and test \(macOS\)" failed at step 12 "go test" \(https:\/\/github\.com\/acme\/flint\/actions\/runs\/41307693761\/job\/124851367741\)/)
    assert.match(p, /```text\n…\n--- FAIL: TestStop \(4\.62s\)\n {4}review_test\.go:316: Stop took 4\.3s\nFAIL\n```/)
    assert.match(p, /40 lines of the step are left out/)
    assert.match(p, /- internal\/remote\/review_test\.go:316\n/)
    assert.match(p, /- internal\/ratelimit\/bucket\.go:42:9\n/)
    assert.match(p, /Job "Lint" failed at step 3 "golangci"\. GitHub returned no log for it/)
    assert.ok(!/workspace is on/.test(p))
    assert.match(p, /Find the cause and fix it/)
  })
  it('warns when the workspace is on another branch', () => {
    const p = ciFixPrompt([report], { pr, workspaceBranch: 'main' })
    assert.match(p, /The pull request's branch is feat\/rate-limit, but this workspace is on main/)
  })
  it('works without a pull request', () => {
    const p = ciFixPrompt([report], {})
    assert.match(p, /^CI failed on branch feat\/rate-limit/)
  })
})

describe('fence', () => {
  it('is longer than any backtick run in the text', () => {
    assert.equal(fence(['a']), '```')
    assert.equal(fence(['x ``` y']), '````')
    assert.equal(fence(['`````']), '``````')
  })
})

describe('ciNotice', () => {
  it('names the pull request and what failed', () => {
    const pr = { number: 29, title: 'feat: rate limiting' }
    assert.deepEqual(ciNotice('failed', pr, ['CI / test', 'CI / lint']), {
      title: 'CI failed: #29 feat: rate limiting',
      body: 'CI / test, CI / lint',
    })
    assert.deepEqual(ciNotice('passed', pr, []), { title: 'CI passed: #29 feat: rate limiting', body: 'All checks passed' })
    assert.equal(ciNotice('failed', pr, ['a', 'b', 'c', 'd']).body, 'a, b, c and 1 more')
  })
})

describe('coerceCI', () => {
  it('fills defaults and clamps like Go', () => {
    assert.deepEqual(coerceCI(undefined), DEFAULT_CI)
    assert.deepEqual(coerceCI({ pollSeconds: 1, logTailLines: 1e9, logErrorLines: -4, notify: false }), {
      pollSeconds: 10,
      logTailLines: 1000,
      logErrorLines: 0,
      notify: false,
    })
    assert.deepEqual(coerceCI({ pollSeconds: 'x', logErrorLines: 0 }), { ...DEFAULT_CI, logErrorLines: 0 })
  })
})
