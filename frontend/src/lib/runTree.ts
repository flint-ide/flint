// Pure helpers for test results streamed by the Go test runner (internal/testrun):
// the tree of packages / suites / tests, counts and what "rerun failed" selects.

export type TestStatus = 'running' | 'passed' | 'failed' | 'skipped' | 'cancelled'

/** One package, suite or test (testrun.Node); later updates replace it. */
export interface TestNode {
  id: string
  parent?: string
  name: string
  kind: 'package' | 'suite' | 'test'
  /** Full test name a rerun selects ("TestA/sub" for Go, "suite test" for node:test). */
  test?: string
  status: TestStatus
  elapsedMs?: number
  output?: string
  /** Directory relative paths in output resolve against. */
  dir?: string
  location?: { path: string; line: number; column?: number }
}

/** What selects tests (testrun.Request). */
export interface TestRequest {
  lang: 'go' | 'js'
  dirs: string[]
  file?: string
  tests?: string[]
}

export interface TestTree {
  nodes: Record<string, TestNode>
  /** Child ids in arrival order; '' holds the roots. */
  children: Record<string, string[]>
}

export const emptyTree: TestTree = { nodes: {}, children: {} }

/**
 * Applies a batch of node updates without mutating the tree. The node map and each
 * touched child list are copied once per batch, so a batch of n siblings is O(n).
 */
export function applyNodes(tree: TestTree, updates: TestNode[]): TestTree {
  if (updates.length === 0) return tree
  const nodes = { ...tree.nodes }
  const children = { ...tree.children }
  const copied = new Set<string>() // parents whose child list is already this batch's copy
  for (const u of updates) {
    if (!nodes[u.id]) {
      const parent = u.parent ?? ''
      if (!copied.has(parent)) {
        children[parent] = [...(children[parent] ?? [])]
        copied.add(parent)
      }
      children[parent].push(u.id)
    }
    nodes[u.id] = u
  }
  return { nodes, children }
}

export interface TestCounts {
  passed: number
  failed: number
  skipped: number
  running: number
  total: number
}

/** Counts tests that have no children (the units that actually ran). */
export function countTests(tree: TestTree): TestCounts {
  const counts: TestCounts = { passed: 0, failed: 0, skipped: 0, running: 0, total: 0 }
  for (const n of Object.values(tree.nodes)) {
    if (n.kind !== 'test' || tree.children[n.id]?.length) continue
    counts.total++
    if (n.status === 'passed') counts.passed++
    else if (n.status === 'failed' || n.status === 'cancelled') counts.failed++
    else if (n.status === 'skipped') counts.skipped++
    else counts.running++
  }
  return counts
}

/**
 * The failures to look at: failed nodes none of whose children failed (a Go
 * test fails with its subtest; the subtest is the interesting one). A package
 * that failed on its own (build error, TestMain) counts too.
 */
export function failedNodes(tree: TestTree): TestNode[] {
  return Object.values(tree.nodes).filter(
    (n) => n.status === 'failed' && !(tree.children[n.id] ?? []).some((c) => tree.nodes[c]?.status === 'failed'),
  )
}

/** The request that reruns only what failed in tree; null when nothing failed. */
export function rerunFailedRequest(tree: TestTree, last: TestRequest): TestRequest | null {
  const failed = failedNodes(tree)
  if (failed.length === 0) return null
  // A failure without a test name (build error) reruns its whole package.
  const tests = failed.some((n) => !n.test) ? [] : unique(failed.map((n) => n.test!))
  if (last.lang === 'go') {
    const dirs = unique(failed.map((n) => n.dir ?? '').filter(Boolean))
    return { lang: 'go', dirs: dirs.length ? dirs : last.dirs, tests }
  }
  return { ...last, tests }
}

/** The request that runs a single node of the tree again. */
export function nodeRequest(node: TestNode, last: TestRequest): TestRequest {
  if (last.lang === 'go') return { lang: 'go', dirs: node.dir ? [node.dir] : last.dirs, tests: node.test ? [node.test] : [] }
  return { ...last, tests: node.test ? [node.test] : [] }
}

// Nodes by full test name, in arrival order, built once per tree (trees are immutable):
// the gutter asks for every mark of every open test file on each update.
const byTest = new WeakMap<TestTree, Map<string, TestNode[]>>()

function testIndex(tree: TestTree): Map<string, TestNode[]> {
  let index = byTest.get(tree)
  if (index) return index
  index = new Map()
  for (const n of Object.values(tree.nodes)) {
    if (n.test === undefined) continue
    const list = index.get(n.test)
    if (list) list.push(n)
    else index.set(n.test, [n])
  }
  byTest.set(tree, index)
  return index
}

/** Status of a test (by its full name) whose package directory contains file. */
export function statusOf(tree: TestTree, file: string, test: string): TestStatus | undefined {
  const dir = file.slice(0, file.lastIndexOf('/'))
  for (const n of testIndex(tree).get(test) ?? []) {
    if (!n.dir || n.dir === dir || dir.startsWith(`${n.dir}/`)) return n.status
  }
  return undefined
}

const unique = <T,>(xs: T[]): T[] => [...new Set(xs)]
