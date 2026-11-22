import { Button, Card, Collapsible, CollapsibleContent, CollapsibleTrigger, Spinner } from '@wunderui/react'
import {
  ArrowLeftRight,
  Brain,
  CheckCircle2,
  CircleDashed,
  Download,
  FileText,
  MinusCircle,
  Pencil,
  Search,
  SquareTerminal,
  Trash2,
  Wrench,
  XCircle,
  type LucideIcon,
} from 'lucide-react'
import { memo, useState, type ReactNode } from 'react'
import { isRunning, type ToolCallStatus } from './status'

/** ACP tool kinds; anything else draws as `other`. */
export type ToolKind = 'read' | 'search' | 'edit' | 'delete' | 'move' | 'execute' | 'fetch' | 'think' | 'other'

const KIND_ICONS: Record<ToolKind, LucideIcon> = {
  read: FileText,
  search: Search,
  edit: Pencil,
  delete: Trash2,
  move: ArrowLeftRight,
  execute: SquareTerminal,
  fetch: Download,
  think: Brain,
  other: Wrench,
}

/** The icon of a tool kind (read, edit, execute…). */
export function ToolKindIcon({ kind }: { kind?: string }) {
  const Icon = KIND_ICONS[(kind ?? 'other') as ToolKind] ?? Wrench
  return <Icon className="fui-tool-kind" aria-hidden />
}

const STATUS_LABELS: Record<ToolCallStatus, string> = {
  pending: 'pending',
  in_progress: 'running',
  completed: 'done',
  failed: 'failed',
  cancelled: 'cancelled',
}

/** A tool call's state; a running one spins only while `live` (a call the agent never closed must not spin forever). */
export function ToolStatusIcon({ status, live }: { status: ToolCallStatus; live?: boolean }) {
  const label = STATUS_LABELS[status]
  if (isRunning(status)) {
    return live ? <Spinner className="fui-tool-status" aria-label={label} /> : <CircleDashed className="fui-tool-status" data-status={status} aria-label={label} />
  }
  const Icon = status === 'completed' ? CheckCircle2 : status === 'failed' ? XCircle : MinusCircle
  return <Icon className="fui-tool-status" data-status={status} aria-label={label} />
}

export type ToolCallProps = {
  kind?: string
  /** The call's title (a command, a path); monospace, cut with an ellipsis. */
  title: ReactNode
  status: ToolCallStatus
  /** The turn is still running: running calls show a spinner. */
  live?: boolean
  /** Next to the title, e.g. a DiffStat. */
  meta?: ReactNode
  /** Shown under the row while it is collapsed: the files it touched. */
  paths?: ReactNode
  /** Input, output and diffs; without any the row doesn't expand. */
  children?: ReactNode
  defaultOpen?: boolean
}

/** One tool call as a compact row in an outlined card; click to see its input, output and diffs. */
export const ToolCall = memo(function ToolCall({ kind, title, status, live, meta, paths, children, defaultOpen = false }: ToolCallProps) {
  const [open, setOpen] = useState(defaultOpen)
  const head = (
    <span className="fui-tool-row">
      <span className="fui-tool-title">{title}</span>
      {meta}
      <ToolStatusIcon status={status} live={live} />
    </span>
  )
  return (
    <Card variant="outlined" size="sm" className="fui-tool" data-status={status}>
      {children ? (
        <Collapsible appearance="row" open={open} onOpenChange={setOpen}>
          <CollapsibleTrigger className="fui-tool-head" icon={<ToolKindIcon kind={kind} />} indicator="chevron">
            {head}
          </CollapsibleTrigger>
          {!open && paths && <div className="fui-tool-paths">{paths}</div>}
          <CollapsibleContent className="fui-tool-details">{children}</CollapsibleContent>
        </Collapsible>
      ) : (
        <>
          <div className="fui-tool-head" data-static>
            <ToolKindIcon kind={kind} />
            {head}
          </div>
          {paths && <div className="fui-tool-paths">{paths}</div>}
        </>
      )}
    </Card>
  )
})

/** A tool's raw input or output; `muted` for output. Selectable, scrolls past 240 px. */
export function ToolCallOutput({ children, muted }: { children: ReactNode; muted?: boolean }) {
  return (
    <pre className="fui-tool-pre" data-muted={muted || undefined}>
      {children}
    </pre>
  )
}

export type ToolDiffLine = { kind: 'add' | 'del' | 'sep'; text: string }

export type ToolDiffProps = {
  /** Above the lines: the file, usually a ChatFileLink. */
  header: ReactNode
  lines: readonly ToolDiffLine[]
  /** More changes than shown: ends with "… more changes". */
  truncated?: boolean
}

/** Changed lines only, red and green, like a compact unified diff. */
export function ToolDiff({ header, lines, truncated }: ToolDiffProps) {
  return (
    <div className="fui-tool-diff">
      {header}
      <pre>
        {lines.map((l, i) => (
          <div key={i} data-kind={l.kind}>
            {l.kind === 'add' ? '+ ' : l.kind === 'del' ? '- ' : ''}
            {l.text}
          </div>
        ))}
        {truncated && <div data-kind="sep">… more changes</div>}
      </pre>
    </div>
  )
}

export type ChatFileLinkProps = {
  /** What to show: the path relative to the workspace. */
  label: string
  /** The full path, as the tooltip. */
  path: string
  onOpen: () => void
}

/** A file path in the chat that opens the file. */
export function ChatFileLink({ label, path, onOpen }: ChatFileLinkProps) {
  return (
    <Button variant="link" size="sm" className="fui-chat-file" onClick={onOpen} title={path}>
      {label}
    </Button>
  )
}
