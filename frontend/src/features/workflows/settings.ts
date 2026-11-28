// The "workflows" key of settings.json (Go: internal/features/workflows/settings.go).
import { defineSection } from '../../lib/settingsSections'
import { nameProblem } from './workflows'

export const DAYS = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] as const
export type Day = (typeof DAYS)[number]

/** Same bounds as Go: at most this many schedules, writable folders and bytes of a schedule's task. */
export const MAX_SCHEDULES = 20
export const MAX_WRITABLE = 10
export const MAX_SCHEDULE_TASK_BYTES = 8000

/** A workflow run Flint starts at a time of day (Go: workflows settings, schedules). */
export interface Schedule {
  name: string
  workflow: string
  /** The workspace folder the run is made in (absolute). */
  folder: string
  task: string
  /** The agent that takes the steps that name none ("" = the default agent). */
  agent: string
  /** Local time, HH:MM. */
  at: string
  /** Empty = every day. */
  days: Day[]
}

export interface WorkflowSettings {
  /** How many times one step is taken before a failing check stops sending the work back (1 to 10). */
  maxAttempts: number
  /** How many times Flint asks a step's chat to finish when its turn ended without a result line (0 to 5; 0 = never). */
  nudges: number
  /** The agent that takes a step again when the step's agent reached its usage limit ("" = pause the run). */
  fallbackAgent: string
  /** Resume a run paused by a limit after this many minutes (0 to 720; 0 = wait for the user). */
  resumeAfterMinutes: number
  /** Script steps run in a sandbox (on by default): they may write only in the run's worktree and temp folders. Agent steps are not affected. */
  sandbox: 'off' | 'on'
  /** The sandbox lets a script use the network. */
  sandboxNetwork: boolean
  /** Extra folders a sandboxed script may write: absolute, or under ~/ (at most 10). */
  sandboxWritable: string[]
  /** Runs started at a time (at most 20). */
  schedules: Schedule[]
}

declare module '../../lib/settingsSections' {
  interface FeatureSettings {
    /** Workflow runs: attempts per step and what happens at a usage limit. */
    workflows: WorkflowSettings
  }
}

export const DEFAULT_WORKFLOW_SETTINGS: WorkflowSettings = Object.freeze({
  maxAttempts: 3,
  nudges: 2,
  fallbackAgent: '',
  resumeAfterMinutes: 0,
  sandbox: 'on',
  sandboxNetwork: false,
  sandboxWritable: Object.freeze([]) as unknown as string[],
  schedules: Object.freeze([]) as unknown as Schedule[],
})

const AGENT_NAME = /^[a-z][a-z0-9-]{0,31}$/

function intIn(v: unknown, min: number, max: number, fallback: number): number {
  return typeof v === 'number' && Number.isInteger(v) && v >= min && v <= max ? v : fallback
}

const TIME = /^([01]\d|2[0-3]):[0-5]\d$/
const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const text = (v: unknown) => (typeof v === 'string' ? v : '')
const byteLength = (s: string) => new TextEncoder().encode(s).length

const cleanPath = (v: string): boolean => v.length < 4096 && !/[\u0000-\u001f\u007f]/.test(v)

/** An absolute folder path without control characters (a schedule's folder). */
export const isFolderPath = (v: string): boolean => v.startsWith('/') && cleanPath(v)

/** A folder the sandbox may write: absolute, or under the home folder as ~/path (Go does not take a bare ~/). */
export const isWritablePath = (v: string): boolean => isFolderPath(v) || (/^~\/.+/.test(v) && cleanPath(v))

/** The days in week order, each once, unknown ones dropped. */
export function normalizeDays(v: unknown): Day[] {
  const given = Array.isArray(v) ? v : []
  return DAYS.filter((d) => given.includes(d))
}

/** A schedule from untrusted JSON, or null when it cannot be used (Go stays the authority and refuses what it does not accept). */
export function coerceSchedule(v: unknown): Schedule | null {
  if (!isObject(v)) return null
  const s: Schedule = {
    name: text(v.name),
    workflow: text(v.workflow),
    folder: text(v.folder),
    task: text(v.task),
    agent: AGENT_NAME.test(text(v.agent)) ? text(v.agent) : '',
    at: text(v.at),
    days: normalizeDays(v.days),
  }
  return nameProblem(s.name) === '' && TIME.test(s.at) && isFolderPath(s.folder) && s.workflow !== '' && s.task.trim() !== '' ? s : null
}

/** At most MAX_SCHEDULES usable schedules, the first of a name winning. */
export function coerceSchedules(v: unknown): Schedule[] {
  const out: Schedule[] = []
  for (const item of Array.isArray(v) ? v : []) {
    const s = coerceSchedule(item)
    if (s && !out.some((o) => o.name === s.name) && out.length < MAX_SCHEDULES) out.push(s)
  }
  return out
}

/** At most MAX_WRITABLE distinct folders (absolute, or under ~/). */
export function coerceWritable(v: unknown): string[] {
  const out: string[] = []
  for (const item of Array.isArray(v) ? v : []) {
    if (typeof item === 'string' && isWritablePath(item) && !out.includes(item) && out.length < MAX_WRITABLE) out.push(item)
  }
  return out
}

/** The reasons a list of folders typed one per line cannot be saved; '' when it can. */
export function writableProblem(lines: readonly string[]): string {
  if (lines.length > MAX_WRITABLE) return `At most ${MAX_WRITABLE} folders.`
  const bad = lines.find((l) => !isWritablePath(l))
  if (bad !== undefined) return `${bad} is not an absolute folder, or one under ~/.`
  const dup = lines.find((l, i) => lines.indexOf(l) !== i)
  return dup !== undefined ? `${dup} is listed twice.` : ''
}

/** The folders of a text field: one per line, blanks dropped. */
export const parseFolders = (draft: string): string[] => draft.split('\n').map((l) => l.trim()).filter(Boolean)

export interface ScheduleProblems {
  name: string
  workflow: string
  folder: string
  task: string
  at: string
}

/** Why a schedule cannot be saved, per field ('' = fine). others are the names already taken. */
export function scheduleProblems(s: Schedule, others: readonly string[]): ScheduleProblems {
  const task = s.task.trim()
  return {
    name: (s.name === '' ? 'Give the schedule a name.' : nameProblem(s.name)) || (others.includes(s.name) ? 'Another schedule has this name.' : ''),
    workflow: s.workflow ? '' : 'Choose a workflow.',
    folder: s.folder ? '' : 'Choose a folder.',
    task: task === '' ? 'Say what should be done.' : byteLength(task) > MAX_SCHEDULE_TASK_BYTES ? `The text is too long (at most ${MAX_SCHEDULE_TASK_BYTES} bytes).` : '',
    at: TIME.test(s.at) ? '' : 'Use a time like 02:30.',
  }
}

/** The first problem in the order the form shows its fields, or '' when there is none. */
export const firstProblem = (p: ScheduleProblems): string => [p.name, p.folder, p.workflow, p.task, p.at].find(Boolean) ?? ''

export const hasProblem = (p: ScheduleProblems): boolean => Object.values(p).some(Boolean)

export function coerceWorkflowSettings(v: unknown): WorkflowSettings {
  if (!isObject(v)) return DEFAULT_WORKFLOW_SETTINGS
  const o = v
  return {
    maxAttempts: intIn(o.maxAttempts, 1, 10, DEFAULT_WORKFLOW_SETTINGS.maxAttempts),
    nudges: intIn(o.nudges, 0, 5, DEFAULT_WORKFLOW_SETTINGS.nudges),
    fallbackAgent: typeof o.fallbackAgent === 'string' && AGENT_NAME.test(o.fallbackAgent) ? o.fallbackAgent : '',
    resumeAfterMinutes: intIn(o.resumeAfterMinutes, 0, 720, 0),
    // The sandbox is on unless the file says a literal "off"; the network only for a literal true.
    sandbox: o.sandbox === 'off' ? 'off' : 'on',
    sandboxNetwork: o.sandboxNetwork === true,
    sandboxWritable: coerceWritable(o.sandboxWritable),
    schedules: coerceSchedules(o.schedules),
  }
}

export const workflowSettings = defineSection('workflows', DEFAULT_WORKFLOW_SETTINGS, coerceWorkflowSettings)
