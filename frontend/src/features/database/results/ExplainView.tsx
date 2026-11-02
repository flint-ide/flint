import { useMemo, useState } from 'react'
import type { ExplainResult, PlanNode } from '../../../api/backend'
import {
  compactCount,
  formatMs,
  nodeLabel,
  parentPaths,
  planMetric,
  planTotal,
  shareOf,
  textParents,
  visiblePlanRows,
  visibleTextLines,
} from '../lib/explainTree'
import { showToast } from '../../../store/dialog'

interface Props {
  result: ExplainResult
  sql: string
}

/** EXPLAIN output: an expandable plan tree (Postgres) or an indented text plan (ClickHouse). */
export function ExplainView({ result, sql }: Props) {
  const copy = async () => {
    await navigator.clipboard.writeText(result.raw)
    showToast(result.plan ? 'Copied plan JSON' : 'Copied plan')
  }
  const title = result.driver === 'clickhouse' ? (result.analyze ? 'EXPLAIN PIPELINE' : 'EXPLAIN PLAN') : result.analyze ? 'EXPLAIN ANALYZE' : 'EXPLAIN'
  const facts = [
    title,
    result.plan && result.analyze ? `planning ${formatMs(result.planningMs)}` : '',
    result.plan && result.analyze ? `execution ${formatMs(result.executionMs)}` : '',
    result.plan && !result.analyze ? `cost ${result.plan.totalCost.toFixed(2)}` : '',
    result.warnings ? `${result.warnings} warning${result.warnings === 1 ? '' : 's'}` : '',
    result.rolledBack ? 'rolled back' : '',
  ].filter(Boolean)

  return (
    <div className="explain-view">
      <div className="result-summary">
        <span className={result.warnings ? 'explain-facts has-warnings' : 'explain-facts'}>{facts.join(' · ')}</span>
        <code className="muted truncate" title={sql}>{sql.replace(/\s+/g, ' ')}</code>
        <span className="spacer" />
        <button className="link" onClick={() => void copy()}>{result.plan ? 'Copy JSON' : 'Copy'}</button>
      </div>
      {result.plan ? <PlanTree root={result.plan} /> : <TextPlan lines={result.lines ?? []} />}
    </div>
  )
}

function PlanTree({ root }: { root: PlanNode }) {
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set())
  const [open, setOpen] = useState<ReadonlySet<string>>(() => new Set())
  const rows = useMemo(() => visiblePlanRows(root, collapsed), [root, collapsed])
  const metric = planMetric(root)
  const total = useMemo(() => planTotal(root, metric), [root, metric])

  const toggle = (set: ReadonlySet<string>, path: string): ReadonlySet<string> => {
    const next = new Set(set)
    if (next.has(path)) next.delete(path)
    else next.add(path)
    return next
  }

  return (
    <div className="plan-tree selectable">
      <div className="plan-row plan-head">
        <span className="plan-node">
          Node
          <span className="plan-tree-actions">
            <button className="link" onClick={() => setCollapsed(new Set())}>Expand all</button>
            <button className="link" onClick={() => setCollapsed(new Set(parentPaths(root)))}>Collapse all</button>
          </span>
        </span>
        <span className="plan-bar" title={metric === 'time' ? 'Share of the total self time' : 'Share of the total self cost'}>
          {metric === 'time' ? 'self time' : 'self cost'}
        </span>
        <span className="num" title="Estimated rows per loop">est. rows</span>
        {root.analyzed && <span className="num" title="Actual rows per loop × loops">actual rows</span>}
        {root.analyzed && <span className="num" title="Inclusive time over all loops">time</span>}
        <span className="num" title="Startup..total cost (estimate)">cost</span>
        {root.analyzed && <span className="num" title="Shared buffers hit / read">buffers</span>}
      </div>
      {rows.map(({ node, path, depth, hasChildren }) => {
        const share = shareOf(node, total, metric)
        const isOpen = open.has(path)
        return (
          <div key={path} className="plan-item">
            <div className={`plan-row${node.warnings.length ? ' warn' : ''}${node.neverExecuted ? ' never' : ''}`} onClick={() => setOpen(toggle(open, path))}>
              <span className="plan-node" style={{ paddingLeft: depth * 16 }}>
                <button
                  className={`chevron${hasChildren ? '' : ' leaf'}`}
                  onClick={(e) => {
                    e.stopPropagation()
                    if (hasChildren) setCollapsed(toggle(collapsed, path))
                  }}
                  aria-label={collapsed.has(path) ? 'Expand' : 'Collapse'}
                >
                  {hasChildren ? (collapsed.has(path) ? '▸' : '▾') : '·'}
                </button>
                {node.warnings.length > 0 && <span className="plan-warn" title={node.warnings.join('\n')}>⚠</span>}
                <span className="plan-label">{nodeLabel(node)}</span>
                {node.neverExecuted && <span className="muted"> (never executed)</span>}
              </span>
              <span className="plan-bar" title={`${(share * 100).toFixed(1)}%`}>
                <span className="bar" style={{ width: `${Math.max(share * 100, share > 0 ? 1 : 0)}%` }} />
                <span className="bar-label">
                  {metric === 'time' ? formatMs(node.selfMs) : node.selfCost.toFixed(1)} · {(share * 100).toFixed(share < 0.1 ? 1 : 0)}%
                </span>
              </span>
              <span className="num">{compactCount(node.planRows)}</span>
              {root.analyzed && (
                <span className="num">{node.analyzed ? `${compactCount(node.actualRows)}${node.actualLoops > 1 ? ` × ${compactCount(node.actualLoops)}` : ''}` : ''}</span>
              )}
              {root.analyzed && <span className="num">{node.analyzed ? formatMs(node.totalMs) : ''}</span>}
              <span className="num" title={`${node.startupCost.toFixed(2)}..${node.totalCost.toFixed(2)}`}>{compactCount(Math.round(node.totalCost))}</span>
              {root.analyzed && (
                <span className="num">{node.buffers ? `${compactCount(node.buffers.sharedHit)} / ${compactCount(node.buffers.sharedRead)}` : ''}</span>
              )}
            </div>
            {isOpen && <PlanDetails node={node} indent={depth * 16 + 22} />}
          </div>
        )
      })}
    </div>
  )
}

function PlanDetails({ node, indent }: { node: PlanNode; indent: number }) {
  const b = node.buffers
  const items: [string, string][] = []
  if (node.filter) items.push(['Filter', node.filter])
  if (node.rowsRemovedByFilter) items.push(['Rows Removed by Filter', compactCount(node.rowsRemovedByFilter)])
  for (const d of node.details) items.push([d.key, d.value])
  if (node.analyzed) {
    items.push(['Actual', `rows ${node.actualRows} × ${node.actualLoops} loops, ${formatMs(node.actualStartupMs)}..${formatMs(node.actualTotalMs)} per loop`])
  }
  items.push(['Estimate', `rows ${node.planRows}, width ${node.planWidth}, cost ${node.startupCost.toFixed(2)}..${node.totalCost.toFixed(2)}`])
  if (b) {
    const parts = [
      `shared hit ${b.sharedHit} read ${b.sharedRead}`,
      b.sharedDirtied || b.sharedWritten ? `dirtied ${b.sharedDirtied} written ${b.sharedWritten}` : '',
      b.localHit || b.localRead ? `local hit ${b.localHit} read ${b.localRead}` : '',
      b.tempRead || b.tempWritten ? `temp read ${b.tempRead} written ${b.tempWritten}` : '',
    ]
    items.push(['Buffers', parts.filter(Boolean).join(', ')])
  }
  return (
    <div className="plan-details" style={{ paddingLeft: indent }}>
      {node.warnings.map((w, i) => (
        <div key={`w${i}`} className="plan-warning">⚠ {w}</div>
      ))}
      {items.map(([k, v], i) => (
        <div key={i}>
          <span className="muted">{k}:</span> <code>{v}</code>
        </div>
      ))}
    </div>
  )
}

function TextPlan({ lines }: { lines: { depth: number; text: string }[] }) {
  const [collapsed, setCollapsed] = useState<ReadonlySet<number>>(new Set())
  const parents = useMemo(() => textParents(lines), [lines])
  const visible = useMemo(() => visibleTextLines(lines, collapsed), [lines, collapsed])
  if (lines.length === 0) return <div className="placeholder muted">Empty plan.</div>
  return (
    <div className="plan-tree text-plan selectable">
      {visible.map((i) => (
        <div key={i} className="plan-row" style={{ paddingLeft: lines[i].depth * 16 }}>
          <button
            className={`chevron${parents[i] ? '' : ' leaf'}`}
            onClick={() => {
              if (!parents[i]) return
              const next = new Set(collapsed)
              if (next.has(i)) next.delete(i)
              else next.add(i)
              setCollapsed(next)
            }}
          >
            {parents[i] ? (collapsed.has(i) ? '▸' : '▾') : '·'}
          </button>
          <code>{lines[i].text}</code>
        </div>
      ))}
    </div>
  )
}
