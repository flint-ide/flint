import { it as test } from 'node:test'
import assert from 'node:assert/strict'
import {
  countSummary, findingAttachments, findingPrompt, findingsPrompt, findingThreads, placed, reanchor, reanchorPath,
  severityCounts, sortFindings, splitLines, type Finding,
} from './agentReview'

const lines = ['package main', '', 'func main() {', '\tx := 1', '}']

const finding = (over: Partial<Finding> = {}): Finding => ({
  id: 'f1', path: 'main.go', line: 4, endLine: 4, severity: 'error', title: 'x is unused',
  explanation: 'Go refuses unused variables.', suggestion: '', lineText: '\tx := 1', before: 'func main() {', after: '}',
  ...over,
})

test('reanchor keeps an unchanged line', () => {
  assert.deepEqual(reanchor(finding(), lines), { at: 4, stale: false })
})

test('reanchor follows the line when lines are added above', () => {
  const moved = ['package main', '', 'import "fmt"', '', 'func main() {', '\tx := 1', '}']
  assert.deepEqual(reanchor(finding(), moved), { at: 6, stale: false })
})

test('reanchor prefers the copy with matching neighbours, then the nearest', () => {
  const dup = ['\tx := 1', 'other', 'func main() {', '\tx := 1', '}', '\tx := 1']
  assert.deepEqual(reanchor(finding(), dup), { at: 4, stale: false })
  const noNeighbours = ['a', '\tx := 1', 'b', 'c', 'd', 'e', 'f', '\tx := 1']
  assert.deepEqual(reanchor(finding(), noNeighbours), { at: 2, stale: false })
})

test('reanchor marks a finding stale when its line is gone', () => {
  const edited = ['package main', '', 'func main() {', '\t_ = 1', '}']
  assert.deepEqual(reanchor(finding(), edited), { at: 4, stale: true })
  assert.deepEqual(reanchor(finding(), null), { at: 4, stale: true })
  assert.deepEqual(reanchor(finding(), ['x']), { at: 1, stale: true })
})

test('reanchor needs a matching neighbour for a blank line', () => {
  const blank = finding({ line: 2, lineText: '', before: 'package main', after: 'func main() {' })
  assert.deepEqual(reanchor(blank, lines), { at: 2, stale: false })
  assert.deepEqual(reanchor(blank, ['a', 'b', '', 'c']), { at: 2, stale: true })
})

test('reanchor leaves file-level and unknown-content findings alone', () => {
  assert.deepEqual(reanchor(finding({ line: 0, endLine: 0 }), []), { at: 0, stale: false })
  const unknown = finding({ lineText: '', before: '', after: '', line: 3 })
  assert.deepEqual(reanchor(unknown, lines), { at: 3, stale: false })
  assert.deepEqual(reanchor(unknown, ['a']), { at: 3, stale: true })
})

test('reanchorPath only touches the file and keeps unchanged objects', () => {
  const a = placed(finding())
  const b = placed(finding({ id: 'f2', path: 'other.go' }))
  const out = reanchorPath([a, b], 'main.go', ['\tx := 1'])
  assert.equal(out[1], b)
  assert.equal(out[0].at, 1)
  const same = reanchorPath([a], 'main.go', lines)
  assert.equal(same[0], a)
})

test('splitLines matches Go', () => {
  assert.deepEqual(splitLines('a\r\nb\n'), ['a', 'b'])
  assert.deepEqual(splitLines(''), [])
  assert.deepEqual(splitLines('a\n\n'), ['a', ''])
})

test('counts and summary', () => {
  const fs = [finding(), finding({ severity: 'warning' }), finding({ severity: 'odd' }), finding({ severity: 'info' })]
  assert.deepEqual(severityCounts(fs), { error: 1, warning: 2, info: 1 })
  assert.equal(countSummary(fs), '1 error, 2 warnings, 1 info')
  assert.equal(countSummary([]), 'no findings')
})

test('sortFindings and findingThreads', () => {
  const fs = [placed(finding({ id: 'b', path: 'b.go', line: 1 })), placed(finding({ id: 'a2', line: 9 })), placed(finding({ id: 'a1', line: 0 }))]
  assert.deepEqual(sortFindings(fs).map((f) => f.id), ['b', 'a1', 'a2'])
  const stale = { ...placed(finding()), stale: true }
  assert.deepEqual(findingThreads([placed(finding()), stale]), [
    { id: 'f1', line: 4, side: 'RIGHT', fileLevel: false },
    { id: 'f1', line: 4, side: 'RIGHT', fileLevel: true },
  ])
})

test('findingPrompt carries location, explanation and suggestion', () => {
  const p = findingPrompt(placed(finding({ suggestion: '\t_ = 1' })))
  assert.match(p, /main\.go:4 \(error\): x is unused/)
  assert.match(p, /Go refuses unused variables\./)
  assert.match(p, /Suggested replacement for line 4:\n```\n\t_ = 1\n```/)
  const stale = findingPrompt({ ...placed(finding()), stale: true })
  assert.match(stale, /changed after the review/)
})

test('findingsPrompt lists findings worst first', () => {
  const p = findingsPrompt([
    placed(finding({ id: 'i', severity: 'info', title: 'nit' })),
    placed(finding({ id: 'e', severity: 'error', title: 'bug', path: 'z.go' })),
  ])
  assert.match(p, /^Fix these 2 findings \(1 error, 1 info\)/)
  assert.ok(p.indexOf('bug') < p.indexOf('nit'))
  assert.equal(findingsPrompt([placed(finding())]), findingPrompt(placed(finding())))
})

test('findingAttachments snips the lines around findings', () => {
  const att = findingAttachments('/repo/main.go', lines, [placed(finding())])
  assert.equal(att.length, 1)
  assert.equal(att[0].path, '/repo/main.go')
  assert.equal(att[0].startLine, 2)
  assert.equal(att[0].endLine, 5)
  assert.deepEqual(findingAttachments('/repo/main.go', lines, [placed(finding({ line: 0 }))]), [{ path: '/repo/main.go' }])
})
