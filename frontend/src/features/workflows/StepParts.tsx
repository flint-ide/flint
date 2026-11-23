import { Badge, IconTile } from '@flint-ide/ui'
import {
  ArrowRight, Bot, Check, Circle, Clock, CornerUpLeft, Flag, Gauge, Hand, LoaderCircle, Pause, Play, RefreshCw, ScanSearch, SkipForward,
  Square, Terminal, Undo2, UserCheck, Workflow, X, type LucideIcon,
} from '@flint-ide/ui/icons'
import { kindTone, type LogKind, type StepLook, type StepStatus } from './stepLook'
import { stepKind, type KindInfo, type Tone } from './workflows'

const KIND_ICONS: Record<KindInfo['icon'], LucideIcon> = {
  agent: Bot,
  script: Terminal,
  review: ScanSearch,
  gate: UserCheck,
  workflow: Workflow,
}

const kindIcon = (kind: string): LucideIcon => KIND_ICONS[stepKind(kind).icon]

/** A step kind's identity: its icon on the kind's tinted kit tile. Decorative: the kind's name is always written next to it. */
export function KindTile({ kind, size = 32 }: { kind: string; size?: 24 | 32 | 36 }) {
  return <IconTile icon={kindIcon(kind)} tone={kindTone(kind)} size={size} className="wf-kind-tile" />
}

/** A step kind as its icon and word ("Agent", "Script", "Review", "Approval"), in the kind's colour. */
export function KindLabel({ kind }: { kind: string }) {
  const info = stepKind(kind)
  const Icon = KIND_ICONS[info.icon]
  return (
    <span className="wf-kind" data-tone={kindTone(kind)}>
      <Icon aria-hidden />
      {info.label}
    </span>
  )
}

/** A state as a kit badge; the tone is one of the badge colors the other panels use. */
export function StateBadge({ tone, title, children, icon }: { tone: Tone; title?: string; children: string; icon?: LucideIcon }) {
  const Icon = icon
  return (
    <Badge color={tone} badgeStyle="light" size="sm" title={title} className="wf-state-badge">
      {Icon && <Icon aria-hidden className="wf-badge-icon" />}
      {children}
    </Badge>
  )
}

const STATUS_ICONS: Record<StepStatus, LucideIcon | null> = {
  none: null,
  pending: Circle,
  running: LoaderCircle,
  queued: Clock,
  waiting: Hand,
  paused: Pause,
  done: Check,
  failed: X,
  stopped: Square,
  skipped: SkipForward,
}

/** The look of a step as a kit badge with its state icon (the step detail, the panel). */
export function LookBadge({ look }: { look: StepLook }) {
  if (look.status === 'none') return null
  return <StateBadge tone={look.tone} icon={STATUS_ICONS[look.status] ?? undefined}>{look.label}</StateBadge>
}

/**
 * The round state mark of a block: a check when done, a turning ring while running (still when the user asks for
 * less motion), a hand when it waits for you… Its words are the block's status label; the mark is decorative.
 */
export function StatusMark({ status }: { status: StepStatus }) {
  const Icon = STATUS_ICONS[status]
  if (!Icon) return null
  return (
    <span className="wf-mark" data-status={status} aria-hidden>
      <Icon />
    </span>
  )
}

/** "fails → back to Implement" with a return mark: where the work goes when this step fails. */
export function ReworkNote({ text }: { text: string }) {
  if (!text) return null
  return (
    <span className="wf-rework" title={text}>
      <CornerUpLeft aria-hidden />
      <span className="truncate">{text}</span>
    </span>
  )
}

const LOG_ICONS: Record<LogKind, LucideIcon> = {
  start: Play,
  pass: Check,
  fail: X,
  wait: Hand,
  pause: Pause,
  resume: Play,
  back: Undo2,
  skip: SkipForward,
  check: RefreshCw,
  limit: Gauge,
  done: Flag,
  stop: Square,
  info: ArrowRight,
}

/** The icon of a log line, in the colour of what happened. */
export function LogMark({ kind }: { kind: LogKind }) {
  const Icon = LOG_ICONS[kind]
  return (
    <span className="wf-log-mark" data-kind={kind} aria-hidden>
      <Icon />
    </span>
  )
}

/**
 * The count of a section head (Needs you, Workflows in …, Schedules, the log): a small muted number, quieter than the
 * kit's pill, so a sidebar's heads do not shout. undefined while the number is not known.
 */
export function SectionCount({ n }: { n: number | undefined }) {
  if (n === undefined) return null
  return <span className="wf-count">{n}</span>
}
