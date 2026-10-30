import { memo, useMemo, useState, type ReactNode } from 'react'
import { Chunk } from '@codemirror/merge'
import { Text } from '@codemirror/state'
import type { Diff, PermissionRequest, PlanEntry, ToolCall, Usage } from '../../lib/agentReducer'
import { useEditor } from '../../store/editor'

const TOOL_ICONS: Record<string, string> = {
  read: '◫', search: '⌕', edit: '✎', delete: '✕', move: '⇄', execute: '❯', fetch: '⇣', think: '…', other: '•',
}
const STATUS_ICONS: Record<string, string> = { pending: '◌', in_progress: '◌', completed: '✓', failed: '✗' }

/** Minimal, safe markdown: fenced code, inline code, bold, headings, lists. No HTML. */
// memo everywhere below: streaming appends to one item; the rest must not re-render.
export const MessageText = memo(function MessageText({ text }: { text: string }) {
  const parts = useMemo(() => text.split(/```[^\n]*\n?/), [text])
  return (
    <div className="msg-text selectable">
      {parts.map((part, i) =>
        i % 2 === 1 ? (
          <pre key={i} className="msg-code">{part.replace(/\n$/, '')}</pre>
        ) : (
          <Paragraphs key={i} text={part} />
        ),
      )}
    </div>
  )
})

function Paragraphs({ text }: { text: string }) {
  const lines = text.split('\n')
  return (
    <>
      {lines.map((line, i) => {
        if (!line.trim()) return i > 0 && i < lines.length - 1 ? <div key={i} className="msg-gap" /> : null
        const heading = /^(#{1,4})\s+(.*)$/.exec(line)
        if (heading) return <div key={i} className="msg-heading">{inline(heading[2])}</div>
        const bullet = /^\s*([-*]|\d+\.)\s+(.*)$/.exec(line)
        if (bullet) return <div key={i} className="msg-bullet"><span>{/\d/.test(bullet[1]) ? bullet[1] : '•'}</span>{inline(bullet[2])}</div>
        return <div key={i}>{inline(line)}</div>
      })}
    </>
  )
}

function inline(s: string): ReactNode[] {
  return s.split(/(`[^`]+`|\*\*[^*]+\*\*)/).map((tok, i) => {
    if (tok.startsWith('`') && tok.endsWith('`') && tok.length > 1) return <code key={i}>{tok.slice(1, -1)}</code>
    if (tok.startsWith('**') && tok.endsWith('**') && tok.length > 3) return <b key={i}>{tok.slice(2, -2)}</b>
    return tok
  })
}

/** One tool call as a compact row; click to see input, output and diffs. */
export const ToolCallRow = memo(function ToolCallRow({ call, root }: { call: ToolCall; root: string }) {
  const [open, setOpen] = useState(false)
  const status = call.status ?? 'pending'
  const hasDetails = !!(call.input || call.output || call.diffs?.length)
  return (
    <div className={`tool-call ${status}`}>
      <div className="tool-head" onClick={() => hasDetails && setOpen(!open)}>
        <span className="tool-icon">{TOOL_ICONS[call.kind ?? 'other'] ?? '•'}</span>
        <span className="tool-title truncate">{call.title || call.kind || 'tool'}</span>
        {(call.diffs?.length ?? 0) > 0 && <DiffStat diffs={call.diffs!} />}
        <span className={`tool-status ${status}`}>{STATUS_ICONS[status] ?? '◌'}</span>
      </div>
      {(call.paths?.length ?? 0) > 0 && !open && (
        <div className="tool-paths">
          {call.paths!.slice(0, 4).map((p) => <FileLink key={p} path={p} root={root} />)}
        </div>
      )}
      {open && (
        <div className="tool-details">
          {call.diffs?.map((d) => <DiffPreview key={d.path} diff={d} root={root} />)}
          {call.input && !call.diffs?.length && <pre className="tool-pre">{call.input}</pre>}
          {call.output && <pre className="tool-pre output">{call.output}</pre>}
        </div>
      )}
    </div>
  )
})

function DiffStat({ diffs }: { diffs: Diff[] }) {
  // Myers diff over whole files is the expensive part; only recompute when diffs change.
  const { add, del } = useMemo(() => countChanges(diffs), [diffs])
  return (
    <span className="diff-stat">
      <span className="add">+{add}</span> <span className="del">−{del}</span>
    </span>
  )
}

function countChanges(diffs: Diff[]): { add: number; del: number } {
  let add = 0
  let del = 0
  for (const d of diffs) {
    const a = Text.of(d.oldText.split('\n'))
    const b = Text.of(d.newText.split('\n'))
    for (const c of Chunk.build(a, b)) {
      if (c.toB > c.fromB) add += b.lineAt(c.endB).number - b.lineAt(c.fromB).number + 1
      if (c.toA > c.fromA) del += a.lineAt(c.endA).number - a.lineAt(c.fromA).number + 1
    }
  }
  return { add, del }
}

const MAX_DIFF_LINES = 80

/** Changed lines only (red/green), like a compact unified diff. */
export function DiffPreview({ diff, root }: { diff: Diff; root: string }) {
  const lines = useMemo(() => {
    const a = Text.of(diff.oldText.split('\n'))
    const b = Text.of(diff.newText.split('\n'))
    const out: { kind: 'add' | 'del' | 'sep'; text: string }[] = []
    for (const c of Chunk.build(a, b)) {
      if (out.length) out.push({ kind: 'sep', text: '⋯' })
      if (c.toA > c.fromA) for (const l of a.sliceString(c.fromA, Math.min(c.toA, a.length)).split('\n')) out.push({ kind: 'del', text: l })
      if (c.toB > c.fromB) for (const l of b.sliceString(c.fromB, Math.min(c.toB, b.length)).split('\n')) out.push({ kind: 'add', text: l })
      if (out.length > MAX_DIFF_LINES) break
    }
    return out
  }, [diff])
  return (
    <div className="diff-preview">
      <FileLink path={diff.path} root={root} />
      <pre>
        {lines.slice(0, MAX_DIFF_LINES).map((l, i) => (
          <div key={i} className={`dl ${l.kind}`}>{l.kind === 'add' ? '+ ' : l.kind === 'del' ? '- ' : ''}{l.text}</div>
        ))}
        {lines.length > MAX_DIFF_LINES && <div className="dl sep">… more changes</div>}
      </pre>
    </div>
  )
}

export function FileLink({ path, root }: { path: string; root: string }) {
  const openFile = useEditor((s) => s.openFile)
  const rel = root && path.startsWith(root + '/') ? path.slice(root.length + 1) : path
  return (
    <button className="link file-link" onClick={() => openFile(path)} title={path}>
      {rel}
    </button>
  )
}

/** A question from the agent with its options; settled requests show the answer. */
export const PermissionCard = memo(function PermissionCard(props: {
  request: PermissionRequest
  answer: string | null
  settled: boolean
  onAnswer: (optionId: string, optionName: string) => void
}) {
  const { request, answer, settled, onAnswer } = props
  const call = request.toolCall
  return (
    <div className={`permission-card${settled ? ' settled' : ''}`}>
      <div className="permission-title">
        <span className="tool-icon">{TOOL_ICONS[call.kind ?? 'other'] ?? '•'}</span>
        <span>{call.title || 'The agent wants to continue'}</span>
      </div>
      {call.input && <pre className="tool-pre">{call.input}</pre>}
      {settled ? (
        <div className="muted permission-answer">{answer ?? 'Cancelled'}</div>
      ) : (
        <div className="permission-actions">
          {request.options.map((o) => (
            <button
              key={o.id}
              className={o.kind.startsWith('allow') ? (o.kind === 'allow_once' ? 'primary' : '') : 'danger'}
              onClick={() => onAnswer(o.id, o.name)}
            >
              {o.name}
            </button>
          ))}
        </div>
      )}
    </div>
  )
})

export function PlanView({ plan }: { plan: PlanEntry[] }) {
  const [open, setOpen] = useState(true)
  if (plan.length === 0) return null
  const done = plan.filter((p) => p.status === 'completed').length
  return (
    <div className="plan-view">
      <div className="plan-head" onClick={() => setOpen(!open)}>
        <span>{open ? '▾' : '▸'} Plan</span>
        <span className="muted">{done}/{plan.length}</span>
      </div>
      {open &&
        plan.map((p, i) => (
          <div key={i} className={`plan-entry ${p.status}`}>
            <span>{p.status === 'completed' ? '☑' : p.status === 'in_progress' ? '◐' : '☐'}</span>
            <span>{p.content}</span>
          </div>
        ))}
    </div>
  )
}

export function UsageLine({ usage, stopReason }: { usage?: Usage; stopReason: string }) {
  const parts: string[] = []
  if (stopReason !== 'end_turn') parts.push(stopReason.replace('_', ' '))
  if (usage && (usage.inputTokens || usage.outputTokens)) parts.push(`${usage.inputTokens + usage.outputTokens} tokens`)
  if (usage?.costUsd) parts.push(`$${usage.costUsd.toFixed(3)}`)
  if (parts.length === 0) return null
  return <div className="turn-end muted">{parts.join(' · ')}</div>
}
