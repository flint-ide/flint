// Workflows (Go: internal/features/workflows): the words and rules of the views, pure and
// testable with node:test. The shapes mirror the Go service's views (the generated binding
// classes fit them structurally). What the user may do with a run is decided in Go
// (RunView.actions); this file only words it and puts it in order.

import type { ScheduleView } from '../../../bindings/flint/internal/features/workflows/models'
import { RUN_VIEW, runStateText, runStateTone, type RunTone } from '../../lib/workflowRuns'

/** The run tab's view id (a TabKind). */
export { RUN_VIEW }

/** Bounds of Go (format.go, run.go, agenttask.MaxPrompt): the same limits, checked before the call for a short answer. The task is counted in bytes. */
export const MAX_TASK_BYTES = 8000
export const MAX_NOTE = 4000
export const MAX_NAME = 64

export interface HoldInfo {
  kind: string
  reason: string
  until?: number
  count?: number
}

export interface StepInfo {
  id: string
  title: string
  kind: string
  agent: string
  run: string
  onFail: string
  text: string
  state: string
  attempts: number
  chat: string
  runTab: string
  output: string
  skipped: boolean
  takenBy: string
  /** A script step in a listing: the command line its run configuration resolves to, or why it cannot be resolved. */
  command?: string
  commandProblem?: string
}

export interface LogInfo {
  at: number
  step?: string
  text: string
}

export interface RunInfo {
  id: string
  workflow: string
  title: string
  description: string
  task: string
  dir: string
  agent: string
  /** Who started it: '' the user, 'agent', 'item', or 'schedule:<name>'. */
  origin: string
  state: string
  error: string
  current: number
  paused: boolean
  feedback: string
  taskState: string
  queued: boolean
  needsUser: boolean
  hold: HoldInfo | null
  actions: string[]
  steps: StepInfo[]
  log: LogInfo[]
  created: number
  updated: number
}

export interface WorkflowInfo {
  name: string
  description: string
  source: string
  path: string
  steps: StepInfo[]
  problem: string
}

/** The kit Badge colors the views use (components/tasks/badges.tsx shares them). */
export type Tone = RunTone

// ---- runs

export const runStateLabel = (run: Pick<RunInfo, 'state' | 'queued'>): string => runStateText(run.state, run.queued)

export const runTone = (run: Pick<RunInfo, 'state' | 'queued'>): Tone => runStateTone(run.state, run.queued)

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
const pad = (n: number) => String(n).padStart(2, '0')
const dayNumber = (d: Date) => Math.floor(new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime() / 86_400_000 + 0.5)

/**
 * A unix time (seconds) as local time: "15:40" today, "tomorrow 02:10", else "3 Feb 15:40".
 * now is the current time in milliseconds.
 */
export function formatWhen(unixSeconds: number, now: number = Date.now()): string {
  const d = new Date(unixSeconds * 1000)
  const clock = `${pad(d.getHours())}:${pad(d.getMinutes())}`
  const days = dayNumber(d) - dayNumber(new Date(now))
  if (days === 0) return clock
  if (days === 1) return `tomorrow ${clock}`
  return `${d.getDate()} ${MONTHS[d.getMonth()]} ${clock}`
}

/** The step the run is at, or null when it is over. */
export function currentStep(run: Pick<RunInfo, 'steps' | 'current'>): StepInfo | null {
  return run.steps[run.current] ?? null
}

/** Who takes a step: the fallback agent, the step's own agent, else the run's. */
export function stepAgent(step: Pick<StepInfo, 'takenBy' | 'agent'>, runAgent: string): string {
  return step.takenBy || step.agent || runAgent
}

/** Hold reasons (Go's text) are shown in one line this long at most. */
const MAX_REASON_LINE = 160

const oneLine = (s: string): string => {
  const line = s.replace(/\s+/g, ' ').trim()
  return [...line].length > MAX_REASON_LINE ? `${[...line].slice(0, MAX_REASON_LINE - 1).join('')}…` : line
}

/** The pause of a run by a limit as a sentence: why and when it goes on by itself. */
export function holdText(run: Pick<RunInfo, 'hold' | 'steps' | 'current' | 'agent'>, now: number = Date.now()): string {
  const hold = run.hold
  if (!hold) return ''
  const step = currentStep(run)
  const who = (step && stepAgent(step, run.agent)) || 'the agent'
  const why = hold.kind === 'budget' ? oneLine(hold.reason) || 'the usage budget is spent' : `${who} reached its usage limit`
  const head = `Paused by a limit: ${why}`
  return hold.until && hold.until > 0 ? `${head} · resumes at ${formatWhen(hold.until, now)}` : head
}

/** What Go says about a limit that holdText does not already show (the budget's reason is in its first line). */
export function holdDetail(hold: HoldInfo | null): string {
  if (!hold) return ''
  return hold.kind === 'budget' && [...hold.reason.replace(/\s+/g, ' ').trim()].length <= MAX_REASON_LINE ? '' : capitalize(hold.reason)
}

/** Who started a run, as a sentence; '' for the user (nothing to say). */
export function originText(origin: string): string {
  if (origin === 'agent') return 'Started by an agent'
  if (origin === 'item') return 'Started from a project item'
  if (origin.startsWith('schedule:') && origin.length > 'schedule:'.length) return `Started by the schedule ${origin.slice('schedule:'.length)}`
  return ''
}

/** The one line a run shows under its title. */
export function runStatus(run: RunInfo, now: number = Date.now()): string {
  const line = statusLine(run, now)
  return run.paused && run.state === 'running' ? `${line} · pauses after this step` : line
}

function statusLine(run: RunInfo, now: number): string {
  const step = currentStep(run)
  const total = run.steps.length
  if (run.hold) return holdText(run, now)
  switch (run.state) {
    case 'done': return `All ${total} ${total === 1 ? 'step' : 'steps'} done`
    case 'stopped': return step ? `Stopped at step ${run.current + 1} of ${total} · ${step.title}` : 'Stopped'
    case 'failed': return step ? `Failed · ${step.title}` : 'Failed'
    case 'paused': return step ? `Paused · next: ${step.title}` : 'Paused'
    case 'waiting':
      if (!step) return 'Waiting for you'
      return step.kind === 'gate' ? `Waiting for your approval · ${step.title}` : `Waiting for you · ${step.title}`
    default:
      if (run.queued) return 'Waits for a free agent slot'
      return step ? `Step ${run.current + 1} of ${total} · ${step.title}` : 'Running'
  }
}

/** True for a run that is over. */
export const isFinished = (run: Pick<RunInfo, 'state'>): boolean => run.state === 'done' || run.state === 'stopped'

export interface RunGroup {
  id: 'needs' | 'running' | 'finished'
  title: string
  runs: RunInfo[]
}

/** Runs for the panel: the ones that need the user first, then the ones going on, then the finished; newest first in each. */
export function groupRuns(runs: readonly RunInfo[]): RunGroup[] {
  const newest = (a: RunInfo, b: RunInfo) => b.updated - a.updated
  const groups: RunGroup[] = [
    { id: 'needs', title: 'Needs you', runs: runs.filter((r) => r.needsUser).sort(newest) },
    { id: 'running', title: 'Running', runs: runs.filter((r) => !r.needsUser && !isFinished(r)).sort(newest) },
    { id: 'finished', title: 'Finished', runs: runs.filter((r) => !r.needsUser && isFinished(r)).sort(newest) },
  ]
  return groups.filter((g) => g.runs.length > 0)
}

/** One run in a workflow's history strip (a kit StatusTracker item). */
export interface HistoryItem {
  id: string
  state: 'ok' | 'failed' | 'warn' | 'running' | 'skipped'
  title: string
}

function historyState(run: Pick<RunInfo, 'state' | 'needsUser'>): HistoryItem['state'] {
  if (run.state === 'done') return 'ok'
  if (run.state === 'failed') return 'failed'
  if (run.state === 'stopped') return 'skipped'
  return run.needsUser || run.state === 'waiting' || run.state === 'paused' ? 'warn' : 'running'
}

/** How many runs a workflow's history strip shows. */
export const HISTORY_RUNS = 10

/** The newest `max` runs of one workflow in one folder, oldest first: what its row shows as a strip. */
export function workflowHistory(runs: readonly RunInfo[], dir: string, workflow: string, max: number = HISTORY_RUNS): HistoryItem[] {
  if (max <= 0) return []
  return runs
    .filter((r) => r.dir === dir && r.workflow === workflow)
    .sort((a, b) => a.created - b.created)
    .slice(-max)
    .map((r) => ({ id: r.id, state: historyState(r), title: `${r.title || r.workflow}: ${runStateLabel(r)}` }))
}

/** How many runs wait for the user: the badge of the activity bar and the status bar item. */
export const needsYouCount = (runs: readonly Pick<RunInfo, 'needsUser'>[]): number => runs.filter((r) => r.needsUser).length

/** "Workflows: 2 need you" (the status bar item). */
export const needsYouText = (n: number): string => `Workflows: ${n} ${n === 1 ? 'needs' : 'need'} you`

export const runTabTitle = (run: Pick<RunInfo, 'title' | 'workflow'>): string => run.title || run.workflow

/**
 * The tooltip of a run's row: its title, then the task when the task says more. A run started from the dialog takes
 * its title from the task, so the two are often the same text: it is shown once.
 */
export function runTooltip(run: Pick<RunInfo, 'title' | 'workflow' | 'description'>): string {
  const title = runTabTitle(run).trim()
  const task = run.description.trim()
  return task && task !== title ? `${title}\n\n${task}` : title
}

// ---- steps

export interface KindInfo {
  label: string
  /** A key of the icon map in StepParts.tsx (this file stays free of React). */
  icon: 'agent' | 'script' | 'review' | 'gate' | 'workflow'
}

const KINDS: Record<string, KindInfo> = {
  agent: { label: 'Agent', icon: 'agent' },
  script: { label: 'Script', icon: 'script' },
  review: { label: 'Review', icon: 'review' },
  gate: { label: 'Approval', icon: 'gate' },
  workflow: { label: 'Workflow', icon: 'workflow' },
}

export const stepKind = (kind: string): KindInfo => KINDS[kind] ?? { label: kind || 'Step', icon: 'agent' }

/** Who or what takes a step, for its card. defaultAgent: the run's agent, or '' in a preview before one is chosen. */
export function stepWho(step: Pick<StepInfo, 'kind' | 'takenBy' | 'agent' | 'run'>, defaultAgent: string): string {
  switch (step.kind) {
    case 'gate': return 'You'
    case 'script': return step.run ? `Runs “${step.run}”` : 'Runs a script'
    default: return stepAgent(step, defaultAgent) || 'The agent you choose'
  }
}

export interface StepBadge {
  label: string
  tone: Tone
}

/**
 * Stopping a run records the step that was running as failed (Go's state): in a stopped run that step was
 * stopped, not failed.
 */
export const stoppedStep = (run: Pick<RunInfo, 'state' | 'current'>, index: number, step: Pick<StepInfo, 'state'>): boolean =>
  run.state === 'stopped' && index === run.current && step.state === 'failed'

/** A step's state as a badge; null in a workflow's definition (no run, no state). stopped: see stoppedStep. */
export function stepBadge(step: Pick<StepInfo, 'state' | 'skipped'>, stopped = false): StepBadge | null {
  if (step.skipped) return { label: 'Continued', tone: 'yellow' }
  if (stopped) return { label: 'Stopped', tone: 'grey' }
  switch (step.state) {
    case 'pending': return { label: 'Not started', tone: 'grey' }
    case 'running': return { label: 'Running', tone: 'blue' }
    case 'done': return { label: 'Done', tone: 'green' }
    case 'failed': return { label: 'Failed', tone: 'red' }
    default: return null
  }
}

/**
 * The label over a step's output: "" when there is nothing to show. A step that is waiting to be taken again
 * (after Send back… or Retry) still holds the last attempt's text, which is labelled as old.
 */
export function outputLabel(step: Pick<StepInfo, 'state' | 'attempts' | 'output'>): string {
  if (!step.output) return ''
  if (step.state !== 'pending') return 'The end of what it said'
  return step.attempts > 0 ? `What it said last time (attempt ${step.attempts})` : ''
}

/** The text with its first letter in upper case (Go's error texts start in lower case). */
export const capitalize = (text: string): string => (text ? text.charAt(0).toUpperCase() + text.slice(1) : text)

/** The step an on-fail key points at (an id or a title). */
export function findStep<T extends Pick<StepInfo, 'id' | 'title'>>(steps: readonly T[], key: string): T | undefined {
  return key ? steps.find((s) => s.id === key || s.title === key) : undefined
}

/** "fails → back to Implement"; '' when the step sends nothing back. */
export function onFailHint(step: Pick<StepInfo, 'onFail'>, steps: readonly Pick<StepInfo, 'id' | 'title'>[]): string {
  if (!step.onFail) return ''
  return `fails → back to ${findStep(steps, step.onFail)?.title ?? step.onFail}`
}

/** "Attempt 2" for a step taken more than once. */
export const attemptsText = (step: Pick<StepInfo, 'attempts'>): string => (step.attempts > 1 ? `Attempt ${step.attempts}` : '')

/** "2 steps". */
export const stepCount = (n: number): string => `${n} ${n === 1 ? 'step' : 'steps'}`

/** The step to show first in a run: the one it is at, else the last. */
export function initialStep(run: Pick<RunInfo, 'steps' | 'current'>): number {
  return Math.max(0, Math.min(run.current, run.steps.length - 1))
}

export interface ChatButton {
  /** What Open chat opens: the step's own chat, the run's agent task and its chat, or nothing. */
  target: 'step' | 'task' | 'none'
  enabled: boolean
  /** The button's tooltip. */
  hint: string
}

/**
 * The Open chat button of a step. Go fills a step's chat only when the step ends, so while the task's own chat
 * still runs the first step, the button opens the run's agent task (as Open does in the Agent Tasks panel).
 */
export function chatButton(run: Pick<RunInfo, 'steps' | 'task' | 'queued'>, index: number): ChatButton {
  const step = run.steps[index]
  if (step?.chat) return { target: 'step', enabled: true, hint: 'Show the chat of this step' }
  if (index === 0 && run.task) {
    if (run.queued) return { target: 'none', enabled: false, hint: 'Waiting for a free slot: the agent has not started yet' }
    return { target: 'task', enabled: true, hint: "Show the agent task and the chat it is working in: this step's own chat is kept when the step ends" }
  }
  return { target: 'none', enabled: false, hint: 'This step has no chat yet' }
}

/** Check again is offered only to a run that failed at an agent or review step that has a chat: the agent may have finished there. */
export const canRecheck = (run: Pick<RunInfo, 'actions'>): boolean => run.actions.includes('recheck')

/** The line under the error of a run that can be checked again. */
export const AGENT_ANSWER_NOTE =
  "Answer the agent in the step's chat, then choose Check again. Retry takes the step again in a new chat; Continue anyway accepts it as it is."

/** The note at the top of a run that is done. */
export const DONE_NOTE = 'All steps are done. Next: look at the changes, then merge them or open a pull request.'

// ---- actions

export interface ActionInfo {
  name: string
  label: string
  tone: 'primary' | 'default' | 'danger'
  /** What it does, in a sentence: the button's tooltip. */
  hint: string
  /** The question asked first ('' = none). */
  confirm: string
  confirmLabel: string
}

const ACTIONS: Record<string, ActionInfo> = {
  approve: { name: 'approve', label: 'Approve', tone: 'primary', hint: 'Pass this step: the run goes on with the next one', confirm: '', confirmLabel: '' },
  resume: { name: 'resume', label: 'Resume', tone: 'primary', hint: 'Let the run go on', confirm: '', confirmLabel: '' },
  recheck: {
    name: 'recheck', label: 'Check again', tone: 'primary',
    hint: "Read the end of the step's chat again: if the agent has finished there, the run goes on", confirm: '', confirmLabel: '',
  },
  retry: { name: 'retry', label: 'Retry', tone: 'primary', hint: 'Take the failed step again', confirm: '', confirmLabel: '' },
  sendBack: { name: 'sendBack', label: 'Send back…', tone: 'default', hint: 'Take an earlier step again, with a note for its agent', confirm: '', confirmLabel: '' },
  skip: {
    name: 'skip', label: 'Continue anyway', tone: 'default', hint: 'Count the failed step as passed and go on',
    confirm: 'Continue although this step failed?\n\nThe step counts as passed and the run goes on with the next one.', confirmLabel: 'Continue Anyway',
  },
  pause: { name: 'pause', label: 'Pause', tone: 'default', hint: 'Pause the run before its next step; the running step ends first', confirm: '', confirmLabel: '' },
  stop: {
    name: 'stop', label: 'Stop', tone: 'danger', hint: 'End the run. The agent task and its files stay',
    confirm: 'Stop this run?\n\nThe step that is running is stopped and the run is over. The agent task and its files stay.', confirmLabel: 'Stop Run',
  },
  remove: {
    name: 'remove', label: 'Remove', tone: 'danger', hint: 'Forget this run. The agent task and its files stay',
    confirm: 'Remove this run from the list?\n\nThe agent task and its files stay.', confirmLabel: 'Remove Run',
  },
}

/** The tooltip of an action for this run: Approve on the last step finishes the run. */
export function actionHint(action: ActionInfo, run: Pick<RunInfo, 'steps' | 'current'>): string {
  return action.name === 'approve' && run.current >= run.steps.length - 1 ? 'Approve and finish the run' : action.hint
}

/** The fixed order of the buttons: what moves the run first, what ends it last. */
const ACTION_ORDER = ['approve', 'resume', 'recheck', 'retry', 'sendBack', 'skip', 'pause', 'stop', 'remove']

/**
 * The actions Go offers, in the fixed order. Names Go may add later and this file does not know are left out.
 * Check again is the primary button when it is offered: Retry is then a plain one.
 */
export function orderedActions(actions: readonly string[]): ActionInfo[] {
  const recheck = actions.includes('recheck')
  return ACTION_ORDER.filter((n) => actions.includes(n)).map((n) => (recheck && n === 'retry' ? { ...ACTIONS[n], tone: 'default' as const } : ACTIONS[n]))
}

/** The action of a run that goes in the panel's row: the first primary one (Approve, Resume, Check again or Retry). */
export function primaryAction(actions: readonly string[]): ActionInfo | null {
  return orderedActions(actions).find((a) => a.tone === 'primary') ?? null
}

export interface SendBackChoice {
  id: string
  title: string
  kind: string
  index: number
}

/** The steps "Send back…" offers: every step from the first up to and including the current one (Go takes any of them). */
export function sendBackChoices(run: Pick<RunInfo, 'steps' | 'current'>): SendBackChoice[] {
  const last = Math.min(run.current, run.steps.length - 1)
  return run.steps.slice(0, last + 1).map((s, index) => ({ id: s.id, title: s.title, kind: s.kind, index }))
}

/** A choice as the dialog lists it: "2. Implement (Agent)". */
export const sendBackLabel = (c: SendBackChoice): string => `${c.index + 1}. ${c.title} (${stepKind(c.kind).label})`

/**
 * The step Send back… starts with: where the current step sends its failure, else the nearest agent step before it,
 * else the last agent step, else (no agent step at all) the nearest step before the current one.
 */
export function defaultSendBack(run: Pick<RunInfo, 'steps' | 'current'>): string {
  const choices = sendBackChoices(run)
  const agents = choices.filter((c) => c.kind === 'agent')
  const step = currentStep(run)
  const target = step && findStep(run.steps, step.onFail)
  if (target && agents.some((c) => c.id === target.id)) return target.id
  const pool = agents.length > 0 ? agents : choices
  const before = pool.filter((c) => c.index < run.current)
  return (before[before.length - 1] ?? pool[pool.length - 1])?.id ?? ''
}

export function noteProblem(note: string): string {
  return [...note].length > MAX_NOTE ? `At most ${MAX_NOTE} characters.` : ''
}

// ---- workflows

/** Where a workflow comes from, in the user's words. */
export function sourceLabel(source: string): string {
  switch (source) {
    case 'builtin': return 'built-in'
    case 'personal': return 'personal'
    case 'project': return 'this repository'
    default: return source
  }
}

/** A workflow can be started when it has no problem and some steps. */
export const usable = (w: Pick<WorkflowInfo, 'problem' | 'steps'>): boolean => !w.problem && w.steps.length > 0

/** The workflow Run Workflow… starts with: the asked one when usable, else the first usable one. */
export function pickWorkflow(list: readonly WorkflowInfo[], wanted: string): string {
  return (list.find((w) => w.name === wanted && usable(w)) ?? list.find(usable))?.name ?? ''
}

/** Why Run Workflow… cannot start yet; '' when it can. */
export function taskProblem(description: string): string {
  const text = description.trim()
  if (text === '') return 'Say what should be done.'
  if (new TextEncoder().encode(text).length > MAX_TASK_BYTES) return `The text is too long (at most ${MAX_TASK_BYTES} bytes).`
  return ''
}

/** Same rule as Go's file names for workflows: lowercase letters, digits and dashes, not starting with a dash. */
export function nameProblem(name: string): string {
  if (name === '') return 'Give the workflow a name.'
  if (name.length > MAX_NAME) return `At most ${MAX_NAME} characters.`
  if (!/^[a-z0-9][a-z0-9-]*$/.test(name)) return 'Use lowercase letters, digits and dashes, and start with a letter or a digit.'
  return ''
}

/** The sentence that says where a run happens. */
export const whereItRuns = (folderName: string): string => `A new branch and worktree of ${folderName}, like an agent task.`

/** Words for a workflow in a folder's list: description, else its steps. */
export function workflowMeta(w: Pick<WorkflowInfo, 'description' | 'source' | 'steps' | 'problem'>): string[] {
  return [w.description, sourceLabel(w.source), w.problem ? '' : stepCount(w.steps.length)].filter(Boolean)
}

/** Settings text of the restricted-mode note. */
export function restrictedNote(hidden: number): string {
  return hidden > 0
    ? `This folder is in Restricted Mode, so its ${hidden === 1 ? 'workflow is' : `${hidden} workflows are`} hidden. Trust the folder to use ${hidden === 1 ? 'it' : 'them'}.`
    : 'This folder is in Restricted Mode, so workflows from the repository are hidden. Trust the folder to use them.'
}

/** The log of a run as lines: time and text, oldest first. */
export function logLines(run: Pick<RunInfo, 'log'>, now: number = Date.now()): { time: string; text: string; key: string }[] {
  return run.log.map((e, i) => ({ time: formatWhen(e.at, now), text: e.text, key: `${e.at}:${i}` }))
}

/** The run id a run tab holds (its data is JSON a session kept); '' when it is not valid. */
export function runIdOf(data: unknown): string {
  const id = typeof data === 'object' && data !== null ? (data as { runId?: unknown }).runId : undefined
  return typeof id === 'string' ? id : ''
}

// ---- Coordinate with Agent…

/** Go's MaxGoal. */
export const MAX_GOAL = 2000

/** Why a goal cannot be sent; '' when it can. */
export function goalProblem(goal: string): string {
  if (goal.trim() === '') return 'Say what the goal is.'
  if ([...goal].length > MAX_GOAL) return `At most ${MAX_GOAL} characters.`
  return ''
}

/** Go refuses a folder in Restricted Mode: the dialog then offers Trust Folder…. */
export const isRestrictedError = (message: string): boolean => /Restricted Mode|not trusted/i.test(message)

/** The note when the workflow tools are off (CoordinatePrompt.toolOn false). The names are the Settings labels. */
export const COORDINATE_TOOL_OFF =
  'The prompt is in the chat draft, but the agent can only start and steer runs after you switch on "Run and steer workflows" in Settings ▸ Agent Context ▸ Actions (and "Share Flint\'s context with agents").'

/** What Coordinate with Agent… does, in one sentence. */
export const COORDINATE_EXPLAIN =
  'An agent starts and watches workflow runs for this goal. It asks you before each action and cannot approve for you. To let it hand out project items too, switch on "Delegate project items" in Settings ▸ Agent Context.'

// ---- schedules

const DAY_NAMES: Record<string, string> = { mon: 'Mon', tue: 'Tue', wed: 'Wed', thu: 'Thu', fri: 'Fri', sat: 'Sat', sun: 'Sun' }

/** "every day", "weekdays" or "Mon, Wed". */
export function daysText(days: readonly string[]): string {
  const known = Object.keys(DAY_NAMES).filter((d) => days.includes(d))
  if (known.length === 0 || known.length === 7) return 'every day'
  if (known.join() === 'mon,tue,wed,thu,fri') return 'weekdays'
  if (known.join() === 'sat,sun') return 'weekends'
  return known.map((d) => DAY_NAMES[d]).join(', ')
}

/** "02:30 every day": when a schedule starts a run. */
export const scheduleWhen = (s: Pick<ScheduleView, 'at' | 'days'>): string => `${s.at} ${daysText(s.days)}`

/** A unix time as "today 02:30", "tomorrow 02:30" or "3 Feb 02:30". */
export function formatDayTime(unixSeconds: number, now: number = Date.now()): string {
  const clock = formatWhen(unixSeconds, now)
  return /^\d\d:\d\d$/.test(clock) ? `today ${clock}` : clock
}

/** "Next: today 02:30", or why there is none. */
export function nextText(s: Pick<ScheduleView, 'next'>, now: number = Date.now()): string {
  return s.next > 0 ? `Next: ${formatDayTime(s.next, now)}` : 'Cannot run now: its folder is not open and trusted in Flint'
}

/** "Did not start last time: the folder is not open in Flint"; '' when the last time started. */
export function skippedText(s: Pick<ScheduleView, 'lastSkipped'>): string {
  const reason = s.lastSkipped.trim().replace(/\.$/, '')
  return reason ? `Did not start last time: ${reason}` : ''
}

/** "Last run: yesterday…" as time; '' when it never started a run. */
export function lastRunText(s: Pick<ScheduleView, 'lastRun' | 'lastAt'>, now: number = Date.now()): string {
  return s.lastRun ? `Last run: ${s.lastAt > 0 ? formatDayTime(s.lastAt, now) : s.lastRun}` : ''
}

/** The lines under a schedule: when it runs next, why it did not start, its last run. */
export function scheduleLines(s: ScheduleView, now: number = Date.now()): string[] {
  return [nextText(s, now), skippedText(s), lastRunText(s, now)].filter(Boolean)
}

/** The warning of a schedule whose workflow or run configuration changed since it was saved. */
export const SCHEDULE_CHANGED_NOTE = 'The workflow or a command it runs changed since you saved this schedule. It will not run until you accept the change.'

// ---- script steps

/** How script steps run (Listing.sandbox). */
export type SandboxState = 'on' | 'off' | 'unavailable'

export interface ScriptLine {
  key: string
  /** The sentence in parts, so the command is drawn as text inside <code>: before, command, after. */
  before: string
  command: string
  after: string
}

export interface ScriptNotice {
  lines: ScriptLine[]
  /** What the sandbox does for these steps. */
  sandbox: string
  /** True when the sandbox is not on: the notice is a warning with a way to the settings. */
  warn: boolean
}

const SANDBOX_TEXT: Record<SandboxState, string> = {
  on: "Script steps run in a sandbox: they can write only in the run's folder and temp folders, and have no network unless you allow it in Settings ▸ Workflows.",
  off: 'Script steps run without a sandbox, with your access to this computer. The sandbox is switched off in Settings ▸ Workflows.',
  unavailable: 'The sandbox is not available on this computer, so script steps will fail: switch off “Run script steps in a sandbox” in Settings ▸ Workflows to run them without one (Flint asks you once per folder).',
}

/** The sentence about the sandbox; '' for a value this file does not know. */
export const sandboxText = (sandbox: string): string => (sandbox in SANDBOX_TEXT ? SANDBOX_TEXT[sandbox as SandboxState] : '')

/** With the sandbox off: what Flint does before the first run in a folder, or that the user already said yes there. */
export const UNSANDBOXED_ASKS = 'Flint asks you once before the first such run in this folder.'
export const UNSANDBOXED_ALLOWED = 'You allowed scripts without a sandbox in this folder.'
export const consentText = (sandbox: string, allowed: boolean): string => (sandbox !== 'off' ? '' : allowed ? UNSANDBOXED_ALLOWED : UNSANDBOXED_ASKS)

/**
 * What the script steps of a workflow run, before the run starts: one line per script step, then the sandbox.
 * null when there is no script step. A command Go could not resolve shows its problem instead; Go decides
 * whether the run may start.
 */
export function scriptNotice(steps: readonly Pick<StepInfo, 'id' | 'title' | 'kind' | 'run' | 'command' | 'commandProblem'>[], sandbox: string): ScriptNotice | null {
  const lines = steps
    .map((s, i) => ({ s, i }))
    .filter(({ s }) => s.kind === 'script')
    .map(({ s, i }): ScriptLine => {
      const key = `${s.id}:${i}`
      if (s.commandProblem) return { key, before: `${s.title} cannot run: ${s.commandProblem}`, command: '', after: '' }
      if (s.command) return { key, before: `${s.title} runs `, command: s.command, after: ' on the code the agent wrote.' }
      return { key, before: `${s.title} runs the run configuration “${s.run}” on the code the agent wrote.`, command: '', after: '' }
    })
  if (lines.length === 0) return null
  return { lines, sandbox: sandboxText(sandbox), warn: sandbox !== 'on' }
}

// ---- the diagram view of one workflow

/** The workflow tab's view id (a TabKind). */
export const WORKFLOW_VIEW = 'workflow-view'

/** What a workflow tab remembers: the folder and the workflow's name (JSON, so a session keeps it). */
export interface WorkflowViewData {
  dir: string
  name: string
}

/** The tab key of a workflow in a folder (one tab each). */
export const workflowViewKey = (dir: string, name: string): string => `${dir}\u0000${name}`

/** The tab's title: "Workflow: <name>". */
export const workflowViewTitle = (name: string): string => `Workflow: ${name}`

/** The folder and name a workflow tab holds; null when its data is not valid. */
export function workflowViewData(data: unknown): WorkflowViewData | null {
  if (typeof data !== 'object' || data === null) return null
  const { dir, name } = data as Record<string, unknown>
  return typeof dir === 'string' && dir && typeof name === 'string' && name ? { dir, name } : null
}

export const WORKFLOW_GONE = 'This workflow is not offered for the folder any more.'

/** The head of the diagram view: the name, the source badge, the description and the step count. */
export interface WorkflowHeader {
  name: string
  source: string
  description: string
  steps: string
}

export function workflowHeader(w: Pick<WorkflowInfo, 'name' | 'source' | 'description' | 'steps'>): WorkflowHeader {
  return { name: w.name, source: sourceLabel(w.source), description: w.description.trim(), steps: stepCount(w.steps.length) }
}

/** What the view shows: still reading, the read failed, the workflow is gone or cannot run, or the diagram. */
export type WorkflowViewState =
  | { kind: 'loading' }
  | { kind: 'error'; text: string }
  | { kind: 'missing'; text: string }
  | { kind: 'ready'; workflow: WorkflowInfo; /** why it cannot run ('' when it can) */ problem: string }

export function workflowViewState(listed: { workflows: readonly WorkflowInfo[] } | null, readError: string, name: string): WorkflowViewState {
  const workflow = listed?.workflows.find((w) => w.name === name)
  if (workflow) return { kind: 'ready', workflow, problem: workflow.problem || (workflow.steps.length === 0 ? 'It has no steps.' : '') }
  if (!listed) return readError ? { kind: 'error', text: readError } : { kind: 'loading' }
  return { kind: 'missing', text: WORKFLOW_GONE }
}

/** The step the diagram selects first: none (the text is shown when a card is picked), clamped when the workflow got shorter. */
export const clampStep = (picked: number | null, count: number): number | null => (picked === null || picked < 0 || picked >= count ? null : picked)

/** Whether a path is a workflow file: <folder>/.flint/workflows/<name>.md. */
export const isWorkflowFile = (path: string): boolean => /(^|\/)\.flint\/workflows\/[^/]+\.md$/i.test(path)

/** The workflow a file defines, by its path as the listing reports it. */
export const workflowForPath = <T extends Pick<WorkflowInfo, 'path'>>(list: readonly T[], path: string): T | undefined =>
  list.find((w) => w.path === path)

/** The folder a workflow file belongs to (the part before .flint/workflows), '' for any other path. */
export function workflowFileRoot(path: string): string {
  const at = path.search(/\/\.flint\/workflows\/[^/]+\.md$/i)
  return at > 0 ? path.slice(0, at) : ''
}

/** What a step without text of its own says: an agent or review step gets the task, a script runs its command, a gate waits. */
export function noTextNote(kind: string): string {
  if (kind === 'script') return 'A script step has no text: it runs the command shown on its card.'
  if (kind === 'gate') return 'An approval step has no text: the run waits here until you approve.'
  return 'This step has no text of its own: the task is what the agent gets.'
}
