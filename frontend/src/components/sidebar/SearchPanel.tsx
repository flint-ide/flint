import { useEffect, useMemo, useRef, useState } from 'react'
import { useVirtualList } from '../../lib/virtualList'
import { useSearch } from '../../store/search'
import { useWorkspace } from '../../store/workspace'
import { useEditor } from '../../store/editor'
import type { SearchMatch } from '../../api/backend'
import { FileIcon } from '../icons/FileIcon'
import { displayPath, labelOf } from '../../lib/workspaceRoots'

const basename = (p: string) => p.slice(p.lastIndexOf('/') + 1)
const dirname = (p: string) => (p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : '')

const ROW_HEIGHT = 22
const DEBOUNCE_MS = 250

// Remembers what was last searched so remounting the panel doesn't re-run the same search.
let lastSearchKey = ''

type Row = { kind: 'file'; path: string; count: number } | { kind: 'match'; match: SearchMatch }

/** Groups matches by file; rows are keyed by absolute path (files of several folders may share a relative path). */
function groupRows(matches: SearchMatch[], collapsed: Set<string>): Row[] {
  const byFile = new Map<string, SearchMatch[]>()
  for (const m of matches) {
    const abs = `${m.root}/${m.path}`
    const list = byFile.get(abs)
    if (list) list.push(m)
    else byFile.set(abs, [m])
  }
  const rows: Row[] = []
  for (const [path, list] of byFile) {
    rows.push({ kind: 'file', path, count: list.length })
    if (!collapsed.has(path)) for (const match of list) rows.push({ kind: 'match', match })
  }
  return rows
}

export function SearchPanel({ focusToken }: { focusToken: number }) {
  const root = useWorkspace((s) => s.root)
  const roots = useWorkspace((s) => s.roots)
  const { params, matches, summary, error, runningId, setParams, run } = useSearch()
  const openFile = useEditor((s) => s.openFile)
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  // Open when a scope is set (file tree ▸ Find in Folder), so it's visible.
  const [showFilters, setShowFilters] = useState(() => !!params.include || !!params.exclude)
  const input = useRef<HTMLInputElement>(null)

  useEffect(() => {
    input.current?.focus()
    input.current?.select()
  }, [focusToken])

  // Re-run (debounced) whenever the query or its options change.
  useEffect(() => {
    if (roots.length === 0) return
    const key = JSON.stringify([roots, params])
    if (key === lastSearchKey) return
    const t = setTimeout(() => {
      lastSearchKey = key
      void run(roots)
    }, DEBOUNCE_MS)
    return () => clearTimeout(t)
  }, [roots, params, run])

  const rows = useMemo(() => groupRows(matches, collapsed), [matches, collapsed])
  const { scrollRef, totalSize, items } = useVirtualList({ count: rows.length, itemSize: ROW_HEIGHT, overscan: 30 })

  const toggleFile = (path: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev)
      if (next.has(path)) next.delete(path)
      else next.add(path)
      return next
    })

  const open = (m: SearchMatch) => {
    const range = m.ranges[0] ?? { start: 0, end: 0 }
    openFile(`${m.root || root}/${m.path}`, { line: m.line, from: range.start, to: range.end })
  }
  const scoped = roots.length > 1 && roots.includes(params.scope)

  return (
    <div className="panel">
      <div className="panel-header">
        <span className="panel-title">Search</span>
      </div>
      <div className="search-form">
        <div className="search-input">
          <input
            ref={input}
            value={params.query}
            placeholder={roots.length > 1 ? 'Search in workspace' : 'Search in project'}
            spellCheck={false}
            onChange={(e) => setParams({ query: e.target.value })}
            onKeyDown={(e) => e.key === 'Enter' && roots.length > 0 && void run(roots)}
          />
          <Toggle on={params.caseSensitive} title="Match case" onClick={() => setParams({ caseSensitive: !params.caseSensitive })}>Aa</Toggle>
          <Toggle on={params.wholeWord} title="Whole word" onClick={() => setParams({ wholeWord: !params.wholeWord })}>W</Toggle>
          <Toggle on={params.regex} title="Regular expression" onClick={() => setParams({ regex: !params.regex })}>.*</Toggle>
          <Toggle on={showFilters} title="File filters" onClick={() => setShowFilters(!showFilters)}>⋯</Toggle>
        </div>
        {scoped && (
          <div className="search-scope">
            <span className="chip">
              in {labelOf(roots, params.scope)}
              <button className="icon" title="Search the whole workspace" onClick={() => setParams({ scope: '', include: '' })}>×</button>
            </span>
          </div>
        )}
        {showFilters && (
          <>
            <input value={params.include} placeholder="include: *.go, src/**" spellCheck={false} onChange={(e) => setParams({ include: e.target.value })} />
            <input value={params.exclude} placeholder="exclude: *_test.go, vendor/**" spellCheck={false} onChange={(e) => setParams({ exclude: e.target.value })} />
          </>
        )}
        <div className="search-status muted">
          {error ? (
            <span className="error">{error}</span>
          ) : runningId ? (
            `Searching… ${matches.length} matches`
          ) : summary ? (
            `${summary.matches} matches in ${summary.files} files${summary.truncated ? ' (truncated)' : ''} · ${summary.elapsedMs} ms`
          ) : null}
        </div>
      </div>
      <div className="tree" ref={scrollRef}>
        <div style={{ height: totalSize, position: 'relative' }}>
          {items.map((vi) => {
            const row = rows[vi.index]
            const style = { transform: `translateY(${vi.start}px)` }
            if (row.kind === 'file') {
              return (
                <div key={`f:${row.path}`} className="tree-row search-file" style={style} onClick={() => toggleFile(row.path)} title={row.path}>
                  <span className="twisty">{collapsed.has(row.path) ? '▸' : '▾'}</span>
                  <FileIcon name={basename(row.path)} />
                  <span className="file-name">{basename(row.path)}</span>
                  <span className="muted path-hint">{dirname(displayPath(roots, row.path))}</span>
                  <span className="badge">{row.count}</span>
                </div>
              )
            }
            const m = row.match
            return (
              <div key={`m:${m.root}/${m.path}:${m.line}:${vi.index}`} className="tree-row search-match" style={style} onClick={() => open(m)}>
                <span className="line-no">{m.line}</span>
                <MatchText match={m} />
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

function Toggle(props: { on: boolean; title: string; onClick: () => void; children: React.ReactNode }) {
  return (
    <button className={`toggle${props.on ? ' on' : ''}`} title={props.title} onClick={props.onClick}>
      {props.children}
    </button>
  )
}

/** Renders a matched line with highlighted ranges, trimmed of leading indentation. */
function MatchText({ match }: { match: SearchMatch }) {
  const text = match.text
  const indent = text.length - text.trimStart().length
  const parts: React.ReactNode[] = []
  let pos = indent
  match.ranges.forEach((r, i) => {
    const start = Math.max(r.start, pos)
    if (start > pos) parts.push(text.slice(pos, start))
    parts.push(<mark key={i}>{text.slice(start, r.end)}</mark>)
    pos = Math.max(pos, r.end)
  })
  parts.push(text.slice(pos))
  return <span className="match-text">{parts}</span>
}
