// How a step and a run LOOK: the visual state of each block, its connector, the identity of each step kind,
// the progress of a run and the kind of each log line. Pure (no React, no DOM), so the rules are tested; the
// words come from workflows.ts (stepBadge, runStateLabel), this file only picks the treatment.

import { actionHint, primaryAction, runStateLabel, stepBadge, stoppedStep, type RunInfo, type StepInfo, type Tone } from './workflows'

/**
 * The state treatment of a step block. 'none' is a step of a definition (a preview: no run, no state).
 * 'waiting', 'paused' and 'queued' are the run's state shown on the step it is at.
 */
export type StepStatus = 'none' | 'pending' | 'running' | 'queued' | 'waiting' | 'paused' | 'done' | 'failed' | 'stopped' | 'skipped'

export interface StepLook {
  status: StepStatus
  /** The state in words ('' for a preview): the label of the block's status. */
  label: string
  /** The kit badge colour of the state. */
  tone: Tone
}

type RunState = Pick<RunInfo, 'state' | 'current' | 'queued' | 'hold'>

const NONE: StepLook = { status: 'none', label: '', tone: 'grey' }

/**
 * The look of step `index`. The step a run is at carries the run's own state when the run waits for the user, is
 * paused or queued (Go keeps such a step "pending": drawn as "Not started" it would hide that the run needs you).
 */
export function stepLook(run: RunState | null, index: number, step: Pick<StepInfo, 'state' | 'skipped'>): StepLook {
  if (!run) return NONE
  const here = index === run.current
  if (here && run.state === 'waiting') return { status: 'waiting', label: runStateLabel(run), tone: 'indigo' }
  if (here && (run.hold || run.state === 'paused') && step.state !== 'done' && step.state !== 'failed') {
    return { status: 'paused', label: runStateLabel({ state: 'paused', queued: false }), tone: 'yellow' }
  }
  if (here && run.queued && run.state === 'running' && step.state !== 'done') return { status: 'queued', label: runStateLabel(run), tone: 'yellow' }
  const badge = stepBadge(step, stoppedStep(run, index, step))
  if (!badge) return NONE
  return { status: statusOf(step, badge.label), label: badge.label, tone: badge.tone }
}

function statusOf(step: Pick<StepInfo, 'state' | 'skipped'>, label: string): StepStatus {
  if (step.skipped) return 'skipped'
  if (label === 'Stopped') return 'stopped'
  switch (step.state) {
    case 'running': return 'running'
    case 'done': return 'done'
    case 'failed': return 'failed'
    default: return 'pending'
  }
}

/** A step the run has gone past: the connector after it is drawn as travelled. */
export const passed = (status: StepStatus): boolean => status === 'done' || status === 'skipped'

/**
 * The connector between step `index` and the next: 'plain' in a preview, 'done' once the run went past the step,
 * else 'todo' (still ahead).
 */
export function linkState(looks: readonly Pick<StepLook, 'status'>[], index: number): 'plain' | 'done' | 'todo' {
  const look = looks[index]
  if (!look || look.status === 'none') return 'plain'
  return passed(look.status) ? 'done' : 'todo'
}

/** The kit IconTile tone that gives each kind of step its identity. No state colour (green, red, blue, yellow) is a kind's. */
export type KindTone = 'purple' | 'pink' | 'neutral' | 'indigo'

export function kindTone(kind: string): KindTone {
  switch (kind) {
    case 'review': return 'pink'
    case 'script': return 'neutral'
    case 'gate': return 'indigo'
    case 'workflow': return 'neutral'
    default: return 'purple'
  }
}

export interface RunProgress {
  /** Steps the run went past (done or continued anyway). */
  done: number
  total: number
  /** 0 – 100, for the kit Progress. */
  percent: number
  /** "2 of 5 steps done". */
  text: string
}

export function runProgress(run: Pick<RunInfo, 'steps'>): RunProgress {
  const total = run.steps.length
  const done = run.steps.filter((s) => s.state === 'done' || s.skipped).length
  const percent = total === 0 ? 0 : Math.round((done / total) * 100)
  return { done, total, percent, text: `${done} of ${total} ${total === 1 ? 'step' : 'steps'} done` }
}

/** What a line of the run's log is about, for its icon (Go's wording: run.go, recheck.go, limits.go). */
export type LogKind = 'start' | 'pass' | 'fail' | 'wait' | 'pause' | 'resume' | 'back' | 'skip' | 'check' | 'limit' | 'done' | 'stop' | 'info'

const LOG_KINDS: readonly [RegExp, LogKind][] = [
  [/^Run done/, 'done'],
  [/^Stopped\b/, 'stop'],
  [/^(Run started|Started )/, 'start'],
  [/^(Passed|Approved)\b/, 'pass'],
  [/^Failed\b/, 'fail'],
  [/^Waiting\b/, 'wait'],
  [/^Paused\b|^Pause requested/, 'pause'],
  [/^Resumed\b/, 'resume'],
  [/^Sent back\b/, 'back'],
  [/^Continued anyway\b/, 'skip'],
  [/usage limit|budget/i, 'limit'],
  [/^Checked again\b/, 'check'],
]

export function logKind(text: string): LogKind {
  return LOG_KINDS.find(([re]) => re.test(text))?.[1] ?? 'info'
}

/**
 * The notice of a run that waits for the user: what its primary action does and, when offered, the other way out.
 * '' when there is no primary action.
 */
export function waitingNote(run: Pick<RunInfo, 'actions' | 'steps' | 'current'>): string {
  const primary = primaryAction(run.actions)
  if (!primary) return ''
  const hint = actionHint(primary, run)
  const back = run.actions.includes('sendBack') ? ' Or send the work back to an earlier step with a note.' : ''
  return `Your turn: ${hint.charAt(0).toLowerCase()}${hint.slice(1)}.${back}`
}
