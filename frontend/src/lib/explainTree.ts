// Pure helpers for the EXPLAIN view: flattening the plan tree for rendering with
// collapsed nodes, per-node shares for the bars, labels and number formatting.
// Framework-free for node:test.

/** The parts of db.PlanNode the view logic needs. */
export interface PlanNodeLike {
  nodeType: string
  relation?: string
  alias?: string
  index?: string
  joinType?: string
  selfCost: number
  selfMs: number
  analyzed: boolean
  children: PlanNodeLike[]
}

export interface PlanRow<N extends PlanNodeLike = PlanNodeLike> {
  node: N
  /** Stable id of the node: child indices from the root, e.g. "0.1.0". */
  path: string
  depth: number
  hasChildren: boolean
}

/** Nodes in display order, skipping the descendants of collapsed paths. */
export function visiblePlanRows<N extends PlanNodeLike>(root: N, collapsed: ReadonlySet<string>): PlanRow<N>[] {
  const out: PlanRow<N>[] = []
  const walk = (node: N, path: string, depth: number) => {
    out.push({ node, path, depth, hasChildren: node.children.length > 0 })
    if (collapsed.has(path)) return
    node.children.forEach((c, i) => walk(c as N, `${path}.${i}`, depth + 1))
  }
  walk(root, '0', 0)
  return out
}

/** Paths of every node with children (for "collapse all"). */
export function parentPaths(root: PlanNodeLike): string[] {
  return visiblePlanRows(root, new Set())
    .filter((r) => r.hasChildren)
    .map((r) => r.path)
}

/** ANALYZE plans are measured by self time, estimates by self cost. */
export type PlanMetric = 'time' | 'cost'

export function planMetric(root: PlanNodeLike): PlanMetric {
  return root.analyzed ? 'time' : 'cost'
}

function selfOf(node: PlanNodeLike, metric: PlanMetric): number {
  return Math.max(0, metric === 'time' ? node.selfMs : node.selfCost)
}

/** Sum of every node's self value: the 100% the bars are measured against. */
export function planTotal(root: PlanNodeLike, metric: PlanMetric): number {
  return selfOf(root, metric) + root.children.reduce((sum, c) => sum + planTotal(c, metric), 0)
}

/** A node's share of the plan's total (0…1). */
export function shareOf(node: PlanNodeLike, total: number, metric: PlanMetric): number {
  if (total <= 0) return 0
  return Math.min(1, selfOf(node, metric) / total)
}

/** "Index Scan using users_pkey on public.users u", "Hash Join (Left)". */
export function nodeLabel(node: PlanNodeLike): string {
  const parts = [node.nodeType]
  if (node.joinType && node.joinType !== 'Inner' && !node.nodeType.includes(node.joinType)) parts.push(`(${node.joinType})`)
  if (node.index) parts.push(`using ${node.index}`)
  if (node.relation) {
    parts.push(`on ${node.relation}`)
    const bare = node.relation.split('.').pop()
    if (node.alias && node.alias !== bare) parts.push(node.alias)
  }
  return parts.join(' ')
}

/** 12 → "12", 12345 → "12.3k", 4.2e6 → "4.2M". */
export function compactCount(n: number): string {
  const abs = Math.abs(n)
  if (abs >= 1e9) return `${trim(n / 1e9)}G`
  if (abs >= 1e6) return `${trim(n / 1e6)}M`
  if (abs >= 1e4) return `${trim(n / 1e3)}k`
  return Number.isInteger(n) ? String(n) : trim(n)
}

/** Milliseconds with sensible precision: 0.012 ms, 4.5 ms, 1.24 s. */
export function formatMs(ms: number): string {
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)} s`
  if (ms >= 10) return `${ms.toFixed(1)} ms`
  if (ms >= 1) return `${ms.toFixed(2)} ms`
  return `${ms.toFixed(3)} ms`
}

function trim(n: number): string {
  return n.toFixed(1).replace(/\.0$/, '')
}

/** Text plans (ClickHouse): which lines have nested lines under them. */
export function textParents(lines: readonly { depth: number }[]): boolean[] {
  return lines.map((l, i) => i + 1 < lines.length && lines[i + 1].depth > l.depth)
}

/** Indices of the text plan lines to show, hiding everything nested under collapsed lines. */
export function visibleTextLines(lines: readonly { depth: number }[], collapsed: ReadonlySet<number>): number[] {
  const out: number[] = []
  let hideBelow = -1 // depth of the collapsed line whose subtree is being skipped
  lines.forEach((l, i) => {
    if (hideBelow >= 0 && l.depth > hideBelow) return
    hideBelow = -1
    out.push(i)
    if (collapsed.has(i)) hideBelow = l.depth
  })
  return out
}
