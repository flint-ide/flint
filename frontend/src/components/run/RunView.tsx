import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Terminal as XTerm, type ILink } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime'
import {
  attachConsole, openRunLink, rerun, rerunFailedTests, fixFailedTestsWithAgent, runTestNode, sendRunInput, setConsoleSize, stopRun, useRun, type RunSession,
} from '../../store/run'
import { countTests, failedNodes, type TestNode, type TestTree } from '../../lib/runTree'
import { findPathLinks } from '../../lib/pathLinks'
import { registerUrlLinks } from '../../lib/terminalLinks'
import { fitTerminal } from '../../lib/terminalFit'
import { focusSoon } from '../../lib/focusSoon'
import { formatDuration } from '../../lib/format'
import { usePersistentNumber } from '../../lib/usePersistentNumber'
import { Splitter } from '../layout/Splitter'
import { currentTerminalOptions, useTerminalSettings } from '../terminal/useTerminalSettings'
import { PlayIcon, RerunIcon, StopIcon } from './RunWidget'

interface Props {
  runKey: string
  active: boolean
}

/** A run tab: toolbar, test results (for test runs) and the console. */
export function RunView({ runKey, active }: Props) {
  const session = useRun((s) => s.sessions[runKey])
  const [hidePassed, setHidePassed] = useState(false)
  const tests = session?.tests
  const noFailures = useMemo(() => !tests || failedNodes(tests).length === 0, [tests])
  if (!session) return null
  const isTests = session.kind === 'tests'

  return (
    <div className="run-view">
      <div className="run-toolbar">
        <button className="icon" title="Rerun (⌃R)" onClick={() => void rerun(runKey)}>
          {session.running ? <RerunIcon /> : <PlayIcon />}
        </button>
        <button className="icon run-stop" title="Stop (⌘F2)" disabled={!session.running} onClick={() => void stopRun(runKey)}>
          <StopIcon />
        </button>
        {isTests && (
          <>
            <button className="icon" title="Rerun failed tests" disabled={session.running || noFailures} onClick={() => void rerunFailedTests()}>
              ✗↻
            </button>
            <button
              className="run-fix"
              title="Send the failures to the agent chat (⌥-click sends right away)"
              disabled={session.running || noFailures}
              onClick={(e) => void fixFailedTestsWithAgent(e.altKey)}
            >
              Fix with agent
            </button>
            <label className="run-check" title="Hide passed tests">
              <input type="checkbox" checked={hidePassed} onChange={(e) => setHidePassed(e.target.checked)} />
              Hide passed
            </label>
          </>
        )}
        <span className="spacer" />
        {isTests && <TestCountsView tree={session.tests} />}
        <RunStatus session={session} />
      </div>
      <div className="run-body">
        {isTests && <TestPanel session={session} hidePassed={hidePassed} />}
        <RunConsole runKey={runKey} active={active} interactive={session.kind === 'config'} />
      </div>
    </div>
  )
}

function RunStatus({ session }: { session: RunSession }) {
  const { exit } = session
  if (session.running) return <span className="run-status running">Running…</span>
  if (!exit) return null
  const label = exit.stopped
    ? 'Stopped'
    : exit.error
      ? 'Failed to start'
      : exit.signal
        ? `Killed (${exit.signal})`
        : `Exit code ${exit.code}`
  const ok = !exit.stopped && !exit.error && !exit.signal && exit.code === 0
  return (
    <span className={`run-status ${ok ? 'ok' : 'failed'}`} title={exit.error}>
      {label} · {formatDuration(exit.elapsedMs)}
    </span>
  )
}

function TestCountsView({ tree }: { tree: TestTree }) {
  const c = useMemo(() => countTests(tree), [tree])
  if (c.total === 0) return null
  return (
    <span className="test-counts">
      {c.failed > 0 && <span className="failed">✗ {c.failed}</span>}
      <span className="passed">✓ {c.passed}</span>
      {c.skipped > 0 && <span className="skipped">↷ {c.skipped}</span>}
      <span className="muted">of {c.total}</span>
    </span>
  )
}

// ---------- test results ----------

const STATUS_ICON: Record<TestNode['status'], string> = {
  running: '◌',
  passed: '✓',
  failed: '✗',
  skipped: '↷',
  cancelled: '⊘',
}

interface Row {
  node: TestNode
  depth: number
  hasChildren: boolean
}

/** Visible rows in tree order; with hidePassed only branches with something else stay. */
function visibleRows(tree: TestTree, collapsed: Set<string>, hidePassed: boolean): Row[] {
  const rows: Row[] = []
  const keep = (id: string): boolean => {
    const n = tree.nodes[id]
    if (!n) return false
    if (!hidePassed || n.status !== 'passed') return true
    return (tree.children[id] ?? []).some(keep)
  }
  const walk = (parent: string, depth: number) => {
    for (const id of tree.children[parent] ?? []) {
      if (!keep(id)) continue
      const children = tree.children[id] ?? []
      rows.push({ node: tree.nodes[id], depth, hasChildren: children.length > 0 })
      if (!collapsed.has(id)) walk(id, depth + 1)
    }
  }
  walk('', 0)
  return rows
}

function TestPanel({ session, hidePassed }: { session: RunSession; hidePassed: boolean }) {
  const [width, setWidth] = usePersistentNumber('flint.testTreeWidth', 340)
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set())
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const panel = useRef<HTMLDivElement>(null)
  const rows = useMemo(() => visibleRows(session.tests, collapsed, hidePassed), [session.tests, collapsed, hidePassed])
  const selected = selectedId ? session.tests.nodes[selectedId] : undefined

  // A new run starts with a fresh tree; when it ends, show the first failure.
  useEffect(() => setSelectedId(null), [session.runId])
  useEffect(() => {
    if (session.running) return
    setSelectedId((cur) => cur ?? failedNodes(session.tests)[0]?.id ?? null)
  }, [session.running, session.tests])

  const toggle = (id: string) =>
    setCollapsed((s) => {
      const next = new Set(s)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  const openLocation = (n: TestNode) => n.location && void openRunLink(session.key, n.location.path, n.location.line, n.location.column)

  return (
    <>
      <div className="test-panel" style={{ width }} ref={panel}>
        <div className="test-tree" role="tree">
          {rows.length === 0 && <div className="placeholder">{session.running ? 'Waiting for results…' : 'No tests reported.'}</div>}
          {rows.map(({ node, depth, hasChildren }) => (
            <div
              key={node.id}
              role="treeitem"
              aria-selected={node.id === selectedId}
              className={`test-row ${node.status}${node.id === selectedId ? ' selected' : ''}`}
              style={{ paddingLeft: 6 + depth * 14 }}
              title={node.test ?? node.name}
              onClick={() => setSelectedId(node.id)}
              onDoubleClick={() => openLocation(node)}
            >
              <span className="test-twisty" onClick={(e) => (e.stopPropagation(), hasChildren && toggle(node.id))}>
                {hasChildren ? (collapsed.has(node.id) ? '▸' : '▾') : ''}
              </span>
              <span className={`test-icon ${node.status}`}>{STATUS_ICON[node.status]}</span>
              <span className="test-name truncate">{node.name}</span>
              {node.elapsedMs ? <span className="test-time">{formatElapsed(node.elapsedMs)}</span> : null}
              <button
                className="icon test-run"
                title={`Run ${node.test ?? node.name}`}
                disabled={session.running}
                onClick={(e) => (e.stopPropagation(), void runTestNode(node))}
              >
                ▶
              </button>
            </div>
          ))}
        </div>
        {selected && (selected.output || selected.location) && <TestDetails runKey={session.key} node={selected} />}
      </div>
      <Splitter
        direction="horizontal"
        onResize={(ratio) => {
          // The tree is the first child of .run-body, so the ratio is its share.
          const total = panel.current?.parentElement?.getBoundingClientRect().width ?? 1000
          setWidth(Math.min(total - 200, Math.max(180, ratio * total)))
        }}
      />
    </>
  )
}

function formatElapsed(ms: number): string {
  if (ms < 1) return '<1 ms'
  return ms < 1000 ? `${Math.round(ms)} ms` : formatDuration(Math.round(ms))
}

/** Failure / skip output of a test with clickable file:line references. */
function TestDetails({ runKey, node }: { runKey: string; node: TestNode }) {
  const lines = (node.output ?? '').split('\n')
  return (
    <div className="test-details selectable">
      {node.location && (
        <button className="link" onClick={() => void openRunLink(runKey, node.location!.path, node.location!.line, node.location!.column)}>
          {node.location.path.split('/').pop()}:{node.location.line}
        </button>
      )}
      <pre>
        {lines.map((line, i) => (
          <div key={i}>{linkify(line, (path, l, c) => void openRunLink(runKey, path, l, c, node.dir))}</div>
        ))}
      </pre>
    </div>
  )
}

function linkify(line: string, open: (path: string, line?: number, column?: number) => void) {
  const links = findPathLinks(line)
  if (links.length === 0) return line || ' '
  const parts: ReactNode[] = []
  let pos = 0
  links.forEach((l, i) => {
    parts.push(line.slice(pos, l.start))
    parts.push(
      <a key={i} className="path-link" onClick={() => open(l.path, l.line, l.column)}>
        {line.slice(l.start, l.end)}
      </a>,
    )
    pos = l.end
  })
  parts.push(line.slice(pos))
  return parts
}

// ---------- console ----------

// Run output is set a point smaller than the terminal font.
const RUN_FONT_DELTA = -1

/** xterm.js bound to the run's output; input goes to the process for configurations. */
function RunConsole({ runKey, active, interactive }: { runKey: string; active: boolean; interactive: boolean }) {
  const host = useRef<HTMLDivElement>(null)
  const term = useRef<XTerm | null>(null)

  useEffect(() => {
    if (!host.current) return
    const xterm = new XTerm({
      ...currentTerminalOptions(RUN_FONT_DELTA),
      cursorBlink: false,
      scrollback: 5_000,
      allowProposedApi: true,
      disableStdin: !interactive,
      convertEol: false,
      macOptionIsMeta: false,
      macOptionClickForcesSelection: true,
    })
    xterm.open(host.current)
    registerUrlLinks(xterm, BrowserOpenURL)
    xterm.registerLinkProvider({
      provideLinks: (lineNo, callback) => {
        const line = xterm.buffer.active.getLine(lineNo - 1)?.translateToString(true) ?? ''
        const links: ILink[] = findPathLinks(line).map((l) => ({
          range: { start: { x: l.start + 1, y: lineNo }, end: { x: l.end, y: lineNo } },
          text: line.slice(l.start, l.end),
          decorations: { underline: true, pointerCursor: true },
          activate: () => void openRunLink(runKey, l.path, l.line, l.column),
        }))
        callback(links)
      },
    })
    xterm.attachCustomKeyEventHandler((e) => {
      if (e.type === 'keydown' && e.metaKey && e.key === 'k') {
        xterm.clear()
        return false
      }
      return !e.metaKey // ⌘ shortcuts belong to the app and the Edit menu
    })
    fitTerminal(xterm)
    term.current = xterm
    setConsoleSize(runKey, xterm.cols, xterm.rows)

    const detach = attachConsole(runKey, { write: (data) => xterm.write(data), reset: () => xterm.reset() })
    const input = xterm.onData((data) => sendRunInput(runKey, data))
    const resize = xterm.onResize(({ cols, rows }) => setConsoleSize(runKey, cols, rows))
    const observer = new ResizeObserver(() => {
      if (host.current && host.current.offsetWidth > 0) fitTerminal(xterm)
    })
    observer.observe(host.current)
    return () => {
      observer.disconnect()
      input.dispose()
      resize.dispose()
      detach()
      xterm.dispose()
      term.current = null
    }
    // One console per tab; `interactive` only changes with the tab's kind.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runKey])

  useTerminalSettings(term, active, RUN_FONT_DELTA)

  useEffect(() => {
    if (!active) return
    return focusSoon(() => {
      if (!term.current) return
      fitTerminal(term.current)
      if (interactive) term.current.focus()
    })
  }, [active, interactive])

  return <div className="run-console" ref={host} />
}
