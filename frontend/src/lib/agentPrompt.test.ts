import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  failedTestsPrompt,
  MAX_FAILED_TESTS,
  MAX_TEST_OUTPUT_LINES,
  CONTEXT_LINES,
  MAX_SNIPPETS,
  appendToDraft,
  codeFence,
  fileProblemsPrompt,
  pickChat,
  problemAttachments,
  problemPrompt,
  relativePath,
  terminalOutputPrompt,
  type Lines,
  type Problem,
} from './agentPrompt'

const ROOT = '/work/app'
const FILE = '/work/app/internal/foo.go'

const doc = (n: number): Lines => ({ lines: n, line: (i) => ({ text: `line ${i}` }) })
const problem = (p: Partial<Problem>): Problem => ({ line: 1, col: 1, endLine: 1, severity: 'error', message: 'bad', ...p })

describe('agent prompts', () => {
  it('makes paths relative to the project only when inside it', () => {
    assert.equal(relativePath(FILE, ROOT), 'internal/foo.go')
    assert.equal(relativePath(FILE, `${ROOT}/`), 'internal/foo.go')
    assert.equal(relativePath('/work/application/x.go', ROOT), '/work/application/x.go')
    assert.equal(relativePath(FILE, ''), FILE)
  })

  it('fences longer than any backtick run inside', () => {
    assert.equal(codeFence('plain'), '```')
    assert.equal(codeFence('a ```` b'), '`````')
  })

  it('describes one problem with location, severity, source and message', () => {
    const p = problem({ line: 12, col: 5, source: 'gopls', message: 'undefined: x\n  more detail' })
    assert.equal(
      problemPrompt(FILE, ROOT, p),
      'Fix this error reported by gopls in internal/foo.go:\n\n- internal/foo.go:12:5 error (gopls): undefined: x\n    more detail',
    )
    assert.equal(problemPrompt(FILE, ROOT, problem({ severity: 'warning' })), 'Fix this warning in internal/foo.go:\n\n- internal/foo.go:1:1 warning: bad')
  })

  it('lists all problems of a file, errors first, with counts', () => {
    const text = fileProblemsPrompt(FILE, ROOT, [
      problem({ line: 3, severity: 'warning', source: 'staticcheck', message: 'unused' }),
      problem({ line: 9, col: 2, message: 'boom' }),
      problem({ line: 1, col: 7, message: 'first' }),
    ])
    assert.equal(
      text,
      'Fix these 3 problems (2 errors, 1 warning) in internal/foo.go:\n\n- 1:7 error: first\n- 9:2 error: boom\n- 3:1 warning (staticcheck): unused',
    )
    assert.equal(fileProblemsPrompt(FILE, ROOT, [problem({})]), problemPrompt(FILE, ROOT, problem({})))
  })

  it('attaches the lines around problems, merging overlaps and clamping to the file', () => {
    const atts = problemAttachments(FILE, doc(40), [problem({ line: 1 }), problem({ line: 4, endLine: 5 }), problem({ line: 30 })])
    assert.deepEqual(atts, [
      { path: FILE, startLine: 1, endLine: 5 + CONTEXT_LINES, text: [1, 2, 3, 4, 5, 6, 7].map((i) => `line ${i}`).join('\n') },
      { path: FILE, startLine: 28, endLine: 32, text: [28, 29, 30, 31, 32].map((i) => `line ${i}`).join('\n') },
    ])
    // A problem reported past the end (stale diagnostics) still yields valid lines.
    assert.deepEqual(problemAttachments(FILE, doc(3), [problem({ line: 9, endLine: 9 })]).map((a) => [a.startLine, a.endLine]), [[1, 3]])
  })

  it('falls back to the file path when problems are spread all over', () => {
    const many = Array.from({ length: MAX_SNIPPETS + 1 }, (_, i) => problem({ line: 1 + i * 10 }))
    assert.deepEqual(problemAttachments(FILE, doc(200), many), [{ path: FILE }])
    assert.deepEqual(problemAttachments(FILE, doc(200), []), [{ path: FILE }])
  })

  it('wraps terminal output in a fence, trimming padding and blank edges', () => {
    assert.equal(
      terminalOutputPrompt('\n$ go test   \nFAIL x\n\n', 'zsh 1'),
      'Output from the terminal "zsh 1":\n\n```\n$ go test\nFAIL x\n```',
    )
    assert.equal(terminalOutputPrompt('a ``` b'), 'Output from the terminal:\n\n````\na ``` b\n````')
  })

  it('appends to the draft after a blank line', () => {
    assert.equal(appendToDraft('', 'fix'), 'fix')
    assert.equal(appendToDraft('  \n', 'fix'), 'fix')
    assert.equal(appendToDraft('please\n', 'fix'), 'please\n\nfix')
    assert.equal(appendToDraft('please', ''), 'please')
  })
})

describe('failedTestsPrompt', () => {
  it('lists each failure with its relative location and output', () => {
    const text = failedTestsPrompt([{ name: 'TestAdd', output: 'want 3, got 4\n', location: { path: '/p/calc_test.go', line: 14 } }], '/p')
    assert.ok(/^Make this failing test pass/.test(text))
    assert.ok(/- TestAdd \(calc_test\.go:14\)/.test(text))
    assert.ok(/```\nwant 3, got 4\n```/.test(text))
  })

  it('keeps the end of long output and counts failures beyond the limit', () => {
    const long = Array.from({ length: MAX_TEST_OUTPUT_LINES + 5 }, (_, i) => `line ${i}`).join('\n')
    const tests = Array.from({ length: MAX_FAILED_TESTS + 2 }, (_, i) => ({ name: `t${i}`, output: i === 0 ? long : '' }))
    const text = failedTestsPrompt(tests, '/p')
    assert.ok(!text.includes('line 4\n') && text.includes(`line ${MAX_TEST_OUTPUT_LINES + 4}`))
    assert.ok(/last 40 lines/.test(text))
    assert.ok(/…and 2 more failed\./.test(text))
  })
})

describe('pickChat', () => {
  const cwds = { a: ROOT, b: `${ROOT}/sub`, other: '/work/other' }

  it('prefers the active chat, then the most recently used one', () => {
    assert.equal(pickChat({ activeId: 'chat:b', backStack: ['chat:a'], order: ['a', 'b'], cwds, root: ROOT }), 'b')
    assert.equal(pickChat({ activeId: 'file:/x', backStack: ['chat:b', 'chat:a', 'term:1'], order: ['a', 'b'], cwds, root: ROOT }), 'a')
  })

  it('falls back to the newest chat and skips other projects and closed chats', () => {
    assert.equal(pickChat({ activeId: 'chat:other', backStack: ['chat:gone'], order: ['a', 'b', 'other'], cwds, root: ROOT }), 'b')
    assert.equal(pickChat({ activeId: null, backStack: [], order: ['other'], cwds, root: ROOT }), null)
  })

  it('accepts any chat when no folder is open', () => {
    assert.equal(pickChat({ activeId: null, backStack: [], order: ['a', 'other'], cwds, root: '' }), 'other')
  })
})
